// Package workerpeer is the authenticated, host-only Worker transport. It owns
// connection identities and isolated execution bindings, never product tasks.
package workerpeer

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

const Prefix = "/worker/v1/"

type Item struct {
	api.Item
	AsyncCallID    string              `json:"asyncCallId,omitempty"`
	AsyncQuestions []api.AsyncQuestion `json:"asyncQuestions,omitempty"`
}
type Approval struct {
	api.Approval
	TaskID   string `json:"taskId"`
	NativeID string `json:"nativeId,omitempty"`
}
type State struct {
	api.WorkState
	Questions []Item `json:"questions,omitempty"`
}
type Observation struct {
	State     State                `json:"state"`
	Approvals []Approval           `json:"approvals"`
	Items     []Item               `json:"items"`
	Revision  string               `json:"revision"`
	Owner     api.WorkCapabilities `json:"owner"`
}

func encodeItem(v api.Item) Item { return Item{v, v.AsyncCallID, v.AsyncQuestions} }
func decodeItem(v Item) api.Item {
	v.Item.AsyncCallID, v.Item.AsyncQuestions = v.AsyncCallID, v.AsyncQuestions
	return v.Item
}
func encodeState(v api.WorkState) State {
	out := State{WorkState: v}
	for _, i := range v.AsyncQuestions {
		out.Questions = append(out.Questions, encodeItem(i))
	}
	return out
}
func decodeState(v State) api.WorkState {
	v.WorkState.AsyncQuestions = nil
	for _, i := range v.Questions {
		v.WorkState.AsyncQuestions = append(v.WorkState.AsyncQuestions, decodeItem(i))
	}
	return v.WorkState
}
func encodeObservation(v api.WorkObservation) Observation {
	out := Observation{State: encodeState(v.State), Revision: v.Revision, Owner: v.Owner}
	for _, i := range v.Items {
		out.Items = append(out.Items, encodeItem(i))
	}
	for _, a := range v.Approvals {
		out.Approvals = append(out.Approvals, Approval{a, a.TaskID, a.NativeID})
	}
	return out
}
func decodeObservation(v Observation) api.WorkObservation {
	out := api.WorkObservation{State: decodeState(v.State), Revision: v.Revision, Owner: v.Owner}
	for _, i := range v.Items {
		out.Items = append(out.Items, decodeItem(i))
	}
	for _, a := range v.Approvals {
		a.Approval.TaskID = a.TaskID
		a.Approval.NativeID = a.NativeID
		out.Approvals = append(out.Approvals, a.Approval)
	}
	return out
}

type Request struct {
	URLSubmission api.WorkURLSubmission  `json:"urlSubmission,omitempty"`
	Contract      int                    `json:"contract"`
	Action        string                 `json:"action"`
	ID            string                 `json:"id,omitempty"`
	Runtime       string                 `json:"runtime,omitempty"`
	Start         api.WorkStart          `json:"start,omitempty"`
	Message       api.TaskMessage        `json:"message,omitempty"`
	Stop          api.WorkStop           `json:"stop,omitempty"`
	Decision      api.WorkDecision       `json:"decision,omitempty"`
	Query         api.WorkOperationQuery `json:"query,omitempty"`
	Closure       api.QuestionClosure    `json:"closure,omitempty"`
	Files         []api.WorkFileContent  `json:"files,omitempty"`
	InputMetadata []api.WorkFileMetadata `json:"inputMetadata,omitempty"`
	Ref           string                 `json:"ref,omitempty"`
}
type Response struct {
	Name         string                        `json:"name,omitempty"`
	Contract     int                           `json:"contract"`
	Peer         string                        `json:"peer"`
	Rejected     bool                          `json:"rejected,omitempty"`
	Error        string                        `json:"error,omitempty"`
	Task         api.Task                      `json:"task"`
	States       []State                       `json:"states,omitempty"`
	Observation  Observation                   `json:"observation"`
	Capabilities api.WorkCapabilities          `json:"capabilities"`
	Messages     []api.TaskConversationMessage `json:"messages,omitempty"`
	Operation    api.WorkOperation             `json:"operation"`
	File         api.WorkFileContent           `json:"file"`
	URL          string                        `json:"url,omitempty"`
	URLPage      api.WorkURLPage               `json:"urlPage"`
}
type Connection struct {
	Address string `json:"address"`
	Peer    string `json:"peer"`
	Token   string `json:"token"`
}
