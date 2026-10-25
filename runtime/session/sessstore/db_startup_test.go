package sessstore_test

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"

	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

// SQLite's globals live for the process lifetime. A fresh test process is
// essential: repeating opens after the driver is warm cannot test startup.
func TestSQLiteConcurrentFirstOpen(t *testing.T) {
	const helperEnv = "AGENTKIT_TEST_SQLITE_FIRST_OPEN"
	if os.Getenv(helperEnv) != "1" {
		for i := 0; i < 3; i++ {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteConcurrentFirstOpen$")
			cmd.Env = append(os.Environ(), helperEnv+"=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fresh-process startup failed: %v\n%s", err, out)
			}
		}
		return
	}
	const workers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			db, err := sessstore.OpenSQLDB("sqlite", fmt.Sprintf("file:startup-%d?mode=memory&cache=shared", i))
			if err != nil {
				t.Error(err)
				return
			}
			defer db.Close()
			if err := db.Ping(); err != nil {
				t.Error(err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
}

func TestOpenSQLiteRejectsInvalidDSN(t *testing.T) {
	db, err := sessstore.OpenSQLDB("sqlite", "file:invalid?vfs=agentkit_missing_vfs")
	if db != nil {
		defer db.Close()
	}
	if err == nil {
		t.Fatal("expected invalid VFS to fail before exposing the database handle")
	}
	// A failed initial connection must not poison later SQLite opens.
	db, err = sessstore.OpenSQLDB("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
}
