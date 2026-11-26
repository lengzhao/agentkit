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

// errBadCursor means the change feed rejected the cursor: malformed (400), or
// older than the retained events (409 resync_required). Either way the adapter
// resynchronizes from the task list.
var errBadCursor = errors.New("taskloom change cursor rejected")

type viewer struct{ Tenant, Subject, Kind string }

type change struct{ TaskID, Kind string }

type changePage struct {
	Changes []change
	Cursor  string
}

// pending is one row of the Agent's own task list. ForMe is set when the
// task's next step waits on the Agent; other rows (e.g. a task blocked on its
// required subtasks) have nothing for it to do yet.
type pending struct {
	TaskID, Title string
	NextActor     string
	ForMe         bool
}

// listRow is the part of a GET /tasks row the adapter reads.
type listRow struct {
	Task      struct{ ID, Title string }
	NextActor string
	ForMe     bool
}

type commitView struct {
	ID, Author, Kind, Body string
	Sequence               int64
}

// taskDetail is one GET /tasks/{id} view: the task and one timeline page in
// ascending order; Earlier tells that older commits precede the page.
type taskDetail struct {
	Task    struct{ ID, Title string }
	Commits []commitView
	Earlier bool
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
	if cursor != "" && (errors.Is(err, errBadRequest) || errors.Is(err, errConflict)) {
		err = fmt.Errorf("%w: %v", errBadCursor, err)
	}
	return p, err
}

// errBadRequest is any 400; only the caller knows which input was rejected.
var errBadRequest = errors.New("bad request")

// errConflict is any 409; only the caller knows which state conflicted.
var errConflict = errors.New("conflict")

// inbox reads the Agent's own tasks (GET /tasks?mine=true, the whole list).
func (c *client) inbox(ctx context.Context) ([]pending, error) {
	var rows []listRow
	if err := c.get(ctx, "/tasks?mine=true", &rows); err != nil {
		return nil, err
	}
	out := make([]pending, 0, len(rows))
	for _, r := range rows {
		out = append(out, pending{TaskID: r.Task.ID, Title: r.Task.Title, NextActor: r.NextActor, ForMe: r.ForMe})
	}
	return out, nil
}

// commitsAfter returns the task title and its commits after a sequence, in
// ascending order. The view carries the newest page; older pages are read
// with ?before= until the page reaches the given sequence. With latestOnly
// only the newest page is read: enough to learn the current sequence when
// the history itself is not reported.
func (c *client) commitsAfter(ctx context.Context, taskID string, after int64, latestOnly bool) (string, []commitView, error) {
	var (
		title  string
		out    []commitView
		before int64
	)
	for {
		path := "/tasks/" + url.PathEscape(taskID)
		if before > 0 {
			path += "?before=" + strconv.FormatInt(before, 10)
		}
		var d taskDetail
		if err := c.get(ctx, path, &d); err != nil {
			return "", nil, err
		}
		if title == "" {
			title = d.Task.Title
		}
		var page []commitView
		for _, cv := range d.Commits {
			if cv.Sequence > after {
				page = append(page, cv)
			}
		}
		out = append(page, out...)
		if latestOnly || !d.Earlier || len(d.Commits) == 0 || len(page) < len(d.Commits) {
			return title, out, nil
		}
		before = d.Commits[0].Sequence
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
	case resp.StatusCode == http.StatusConflict:
		return fmt.Errorf("%w: GET %s: %s", errConflict, path, strings.TrimSpace(string(body)))
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
