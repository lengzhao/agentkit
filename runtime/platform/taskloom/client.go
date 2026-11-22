package taskloom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// client reads the shared Taskloom business API with the Agent's own
// credential. It only reads: every write goes through the Agent's tools, so
// the adapter can never act on a task by itself.
type client struct {
	base          string
	authorization string
	http          *http.Client
}

// errGone is a task the Agent may no longer read (404/403): removed from the
// task, or the task is outside its tenant now.
var errGone = errors.New("task not readable")

// errAuth means the credential itself is rejected; retrying cannot help until
// an operator issues a new one.
var errAuth = errors.New("taskloom credential rejected")

// errBadCursor means the change feed rejected the cursor (400): it does not
// belong to this database any more, so the adapter resynchronizes.
var errBadCursor = errors.New("taskloom change cursor rejected")

type viewer struct{ Tenant, Subject, Kind string }

type change struct{ TaskID, Kind string }

type changePage struct {
	Changes []change
	Cursor  string
}

type pending struct {
	TaskID, Title     string
	NextActor         string
	WaitingOnSubtasks bool
}

type commitView struct {
	ID, Author, Kind, Body string
	Sequence               int64
}

type taskDetail struct {
	Task    struct{ ID, Title string }
	Commits []commitView
}

func (c *client) me(ctx context.Context) (viewer, error) {
	var v viewer
	return v, c.get(ctx, "/me", &v)
}

func (c *client) changes(ctx context.Context, cursor string) (changePage, error) {
	var p changePage
	path := "/changes"
	if cursor != "" {
		path += "?cursor=" + url.QueryEscape(cursor)
	}
	err := c.get(ctx, path, &p)
	if cursor != "" && errors.Is(err, errBadRequest) {
		err = fmt.Errorf("%w: %v", errBadCursor, err)
	}
	return p, err
}

// errBadRequest is any 400; only the caller knows which input was rejected.
var errBadRequest = errors.New("bad request")

func (c *client) inbox(ctx context.Context) ([]pending, error) {
	var out []pending
	return out, c.get(ctx, "/inbox", &out)
}

// commitsAfter pages through the task timeline after a sequence. The server
// returns at most 100 commits per page, so a short page is the last one.
func (c *client) commitsAfter(ctx context.Context, taskID string, after int64) (string, []commitView, error) {
	var (
		title string
		out   []commitView
	)
	for {
		var d taskDetail
		if err := c.get(ctx, "/tasks/"+url.PathEscape(taskID)+"?after="+strconv.FormatInt(after, 10), &d); err != nil {
			return "", nil, err
		}
		title = d.Task.Title
		out = append(out, d.Commits...)
		if len(d.Commits) < 100 {
			return title, out, nil
		}
		after = d.Commits[len(d.Commits)-1].Sequence
	}
}

func (c *client) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authorization)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errAuth
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: GET %s: %d", errGone, path, resp.StatusCode)
	case resp.StatusCode == http.StatusBadRequest:
		return fmt.Errorf("%w: GET %s: %s", errBadRequest, path, strings.TrimSpace(string(body)))
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, into)
}

func newClient(base, authorization string, timeout time.Duration) *client {
	return &client{
		base:          strings.TrimRight(base, "/"),
		authorization: authorization,
		http:          &http.Client{Timeout: timeout},
	}
}
