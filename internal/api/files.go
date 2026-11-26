package api

import (
	"context"
	"errors"
	"io/fs"

	"gitlab.com/scabb-island/omaslack/internal/domain"
	"gitlab.com/scabb-island/omaslack/internal/provider"
	"gitlab.com/scabb-island/omaslack/internal/store"
	"gitlab.com/scabb-island/omaslack/internal/syncengine"
	"gitlab.com/scabb-island/omaslack/internal/wire"
)

type uploadResult struct {
	Files []wire.File `json:"files"`
}

func filesUpload(ctx context.Context, p *params) (any, error) {
	t := p.target()
	thread := p.optTS("threadTS")
	paths := p.paths()
	caption := p.optString("caption")
	if err := needs(p, t.w, domain.Files); err != nil {
		return nil, err
	}
	files, err := t.w.Engine.UploadFiles(ctx, t.conv, thread, paths, caption)
	if err != nil {
		return nil, fileError("paths", err)
	}
	out := uploadResult{Files: make([]wire.File, 0, len(files))}
	for _, f := range files {
		out.Files = append(out.Files, wire.FromFile(f))
	}
	return out, nil
}

type fetchResult struct {
	LocalPath string `json:"localPath"`
}

func filesFetch(ctx context.Context, p *params) (any, error) {
	w := p.workspace()
	id := parsed(p, "fileId", domain.ParseFileID)
	if err := needs(p, w, domain.Files); err != nil {
		return nil, err
	}
	path, err := w.Engine.FetchFile(ctx, id)
	if err != nil {
		return nil, fileError("fileId", err)
	}
	return fetchResult{LocalPath: path}, nil
}

// fileProblems are the file errors a client can act on, and what it is
// told. Anything else stays an internal error whose text is hidden.
var fileProblems = []struct {
	err  error
	what string
}{
	{store.ErrUnknownFile, "unknown file"},
	{provider.ErrNotDownloadable, "the file cannot be downloaded"},
	{provider.ErrFileTooLarge, "the file is over the size limit"},
	{syncengine.ErrNotRegularFile, "not a regular file"},
	{fs.ErrNotExist, "no such file"},
	{fs.ErrPermission, "permission denied"},
}

// fileError turns a file problem with member name into -32602.
func fileError(name string, err error) error {
	for _, fp := range fileProblems {
		if errors.Is(err, fp.err) {
			return invalid("%s: %s", name, fp.what)
		}
	}
	return err
}
