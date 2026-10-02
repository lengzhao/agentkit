package agenttest

import (
	"context"

	capfs "github.com/lengzhao/agentkit/cap/filesystem"
)

// RecordingFS implements cap/filesystem.Service and counts inner method calls.
type RecordingFS struct {
	ReadCalls   int
	StatCalls   int
	ListCalls   int
	WriteCalls  int
	AppendCalls int
	GrepCalls   int
	FindCalls   int
}

func (r *RecordingFS) Read(context.Context, string) ([]byte, error) {
	r.ReadCalls++
	return []byte("ok"), nil
}

func (r *RecordingFS) Stat(context.Context, string) (capfs.Info, error) {
	r.StatCalls++
	return capfs.Info{}, nil
}

func (r *RecordingFS) List(context.Context, string) ([]capfs.DirEntry, error) {
	r.ListCalls++
	return nil, nil
}

func (r *RecordingFS) Write(context.Context, string, []byte, ...capfs.WriteOption) error {
	r.WriteCalls++
	return nil
}

func (r *RecordingFS) Append(context.Context, string, []byte) error {
	r.AppendCalls++
	return nil
}

func (r *RecordingFS) Grep(context.Context, capfs.GrepRequest) (capfs.GrepResult, error) {
	r.GrepCalls++
	return capfs.GrepResult{}, nil
}

func (r *RecordingFS) Find(context.Context, capfs.FindRequest) (capfs.FindResult, error) {
	r.FindCalls++
	return capfs.FindResult{}, nil
}

var _ capfs.Service = (*RecordingFS)(nil)
