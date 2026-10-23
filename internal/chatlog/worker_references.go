package chatlog

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"sort"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

// WorkerFileReferences reads only submission and artifact identities. It never
// loads chat text into this maintenance path, and the visible 200-item window
// cannot prove that an older attachment is unreferenced.
func (l *Log) WorkerFileReferences(ctx context.Context) (workerfiles.Protection, error) {
	out := workerfiles.Protection{}
	if l == nil {
		return out, nil
	}
	if err := lockwait.Lock(ctx, &l.mu); err != nil {
		return out, err
	}
	defer l.mu.Unlock()
	submissions, refs := map[string]bool{}, map[api.TaskFileReference]bool{}
	addArtifact := func(id string) {
		if id == "" {
			return
		}
		ref, ok := api.ParseTaskArtifactID(id)
		if !ok {
			ref = api.TaskFileReference{ID: id}
		}
		refs[ref] = true
	}
	for _, item := range l.dirty {
		if item.RequestID != "" {
			submissions[item.RequestID] = true
		}
		for _, artifact := range item.Artifacts {
			addArtifact(artifact.ID)
		}
	}
	finish := func(err error) (workerfiles.Protection, error) {
		for id := range submissions {
			out.Submissions = append(out.Submissions, id)
		}
		for ref := range refs {
			out.References = append(out.References, ref)
		}
		sort.Strings(out.Submissions)
		sort.Slice(out.References, func(i, j int) bool {
			a, b := out.References[i], out.References[j]
			return a.TaskID < b.TaskID || a.TaskID == b.TaskID && a.ID < b.ID
		})
		return out, err
	}
	if !l.loaded || l.issue != nil {
		return finish(l.issue)
	}
	u := url.URL{Scheme: "file", Path: l.path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return finish(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return finish(err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT json_extract(body,'$.requestId') FROM messages WHERE json_type(body,'$.requestId') = 'text'`)
	if err != nil {
		return finish(err)
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		if id != "" {
			submissions[id] = true
		}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return finish(err)
	}
	rows, err = tx.QueryContext(ctx, `SELECT json_extract(a.value,'$.id') FROM messages, json_each(body,'$.artifacts') AS a WHERE json_type(a.value,'$.id') = 'text'`)
	if err != nil {
		return finish(err)
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		addArtifact(id)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err == nil {
		err = tx.Commit()
	}
	out.Complete = err == nil
	return finish(err)
}
