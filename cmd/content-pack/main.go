// content-pack is the public, offline creator tool. It shares the app validator.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/contentpack"
	"os"
	"path/filepath"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: content-pack init|pack|verify|unpack (use <command> -h)")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "init":
		dir := fs.String("dir", "", "new source directory")
		id := fs.String("id", "", "namespace.package")
		name := fs.String("name", "", "display name")
		author := fs.String("author", "", "creator attribution")
		license := fs.String("license", "", "license identifier")
		licenseFile := fs.String("license-file", "", "license text to bundle")
		model := fs.String("model", "", "self-contained basic GLB (optional for avatar-only packs)")
		avatar := fs.String("avatar", "", "PNG avatar (optional)")
		if e := fs.Parse(args[1:]); e != nil {
			return e
		}
		if *dir == "" || *licenseFile == "" || (*model == "" && *avatar == "") {
			return errors.New("provide --dir, --license-file, and at least --model or --avatar")
		}
		p := &contentpack.Pack{Manifest: contentpack.Manifest{Format: "caelis-content", FormatVersion: 1, ID: *id, Version: "1.0.0", Name: *name, Author: *author, License: *license, LicenseFile: "LICENSE.txt"}, Files: map[string][]byte{}}
		add := func(source, dest string) error {
			info, e := os.Stat(source)
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() || info.Size() > contentpack.MaxFile {
				return errors.New("source file too large or not regular")
			}
			b, e := os.ReadFile(source)
			if e != nil {
				return e
			}
			p.Files[dest] = b
			p.Manifest.Files = append(p.Manifest.Files, contentpack.File{Path: dest, SHA256: contentpack.Hash(b), Size: int64(len(b))})
			return nil
		}
		if e := add(*licenseFile, "LICENSE.txt"); e != nil {
			return e
		}
		avatarID := ""
		if *avatar != "" {
			if e := add(*avatar, "avatar.png"); e != nil {
				return e
			}
			avatarID = "portrait"
			p.Manifest.Avatars = []contentpack.Avatar{{ID: avatarID, Name: *name, Image: "avatar.png", Capability: "png-v1"}}
		}
		if *model != "" {
			if e := add(*model, "model.glb"); e != nil {
				return e
			}
			p.Manifest.Characters = []contentpack.Character{{ID: "character", Name: *name, Variants: []contentpack.Variant{{ID: "default", Name: "Default", Model: "model.glb", Avatar: avatarID, Capability: "basic-3d-v1"}}}}
		}
		if e := contentpack.Verify(p); e != nil {
			return e
		}
		if e := contentpack.WriteSource(*dir, p); e != nil {
			return e
		}
		fmt.Println("Created", *dir)
		return nil
	case "pack":
		out := fs.String("out", "", "new .caelispack output path")
		if e := fs.Parse(args[1:]); e != nil {
			return e
		}
		if fs.NArg() != 1 || filepath.Ext(*out) != ".caelispack" {
			return errors.New("usage: content-pack pack --out NEW.caelispack SOURCE_DIRECTORY")
		}
		p, e := contentpack.ReadDirectory(fs.Arg(0))
		if e != nil {
			return e
		}
		b, e := contentpack.Encode(p)
		if e != nil {
			return e
		}
		if e = contentpack.WriteArchive(*out, b); e != nil {
			return e
		}
		fmt.Println("Packed", p.Manifest.ID, p.Manifest.Version, contentpack.Hash(b))
		return nil
	case "verify":
		if e := fs.Parse(args[1:]); e != nil {
			return e
		}
		if fs.NArg() != 1 {
			return errors.New("usage: content-pack verify FILE.caelispack")
		}
		p, b, e := contentpack.ReadFile(fs.Arg(0))
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"id": p.Manifest.ID, "version": p.Manifest.Version, "sha256": contentpack.Hash(b), "characters": len(p.Manifest.Characters), "avatars": len(p.Manifest.Avatars), "valid": true})
	case "unpack":
		out := fs.String("out", "", "new source directory")
		if e := fs.Parse(args[1:]); e != nil {
			return e
		}
		if fs.NArg() != 1 || *out == "" {
			return errors.New("usage: content-pack unpack --out NEW_DIRECTORY FILE.caelispack")
		}
		p, _, e := contentpack.ReadFile(fs.Arg(0))
		if e != nil {
			return e
		}
		if e = contentpack.WriteSource(*out, p); e != nil {
			return e
		}
		fmt.Println("Unpacked validated content. Preserve the bundled license and attribution when creating derivatives.")
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}
