package main

import (
	"bytes"
	"errors"
	"io"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("grant-workspaces command", func() {
	ginkgo.It("default seams read process args and create the concrete grant store", func() {
		Expect(grantWorkspaceArgs()).NotTo(BeNil())
		Expect(newGrantStore("/tmp/wikid.db")).To(BeAssignableToTypeOf(wikid.NewGrantStore("")))
	})

	ginkgo.It("main delegates to the runner and exits with its status", func() {
		restore := restoreGrantWorkspaceSeams()
		defer restore()
		var stderr bytes.Buffer
		var exitCode int
		store := &fakeGrantStore{}
		grantWorkspaceArgs = func() []string {
			return []string{"--db-path", "/tmp/wikid.db"}
		}
		grantWorkspaceStdin = strings.NewReader("[]")
		grantWorkspaceStderr = &stderr
		grantWorkspaceExit = func(code int) {
			exitCode = code
			panic("exit")
		}
		newGrantStore = func(path string) grantStore {
			Expect(path).To(Equal("/tmp/wikid.db"))
			return store
		}

		Expect(func() { main() }).To(PanicWith("exit"))

		Expect(exitCode).To(Equal(0))
		Expect(stderr.String()).To(BeEmpty())
	})

	ginkgo.It("upserts grants using a db path derived from the global data dir", func() {
		restore := restoreGrantWorkspaceSeams()
		defer restore()
		var stderr bytes.Buffer
		store := &fakeGrantStore{}
		var storePath string
		newGrantStore = func(path string) grantStore {
			storePath = path
			return store
		}

		code := runGrantWorkspaces(
			[]string{"--global-data-dir", "/tmp/global"},
			strings.NewReader(`[{"subject":"frontd","workspaceId":"home","role":"admin"}]`),
			&stderr,
		)

		Expect(code).To(Equal(0), stderr.String())
		Expect(storePath).To(Equal(wikid.GlobalLayout("/tmp/global").DBPath))
		Expect(store.grants).To(Equal([]wikid.Grant{{
			Subject:     "frontd",
			WorkspaceID: workspaceid.WorkspaceID("home"),
			Role:        wikid.GrantRole("admin"),
		}}))
	})

	ginkgo.It("returns formatted failures for invalid inputs and store errors", func() {
		cases := []struct {
			name    string
			args    []string
			stdin   io.Reader
			store   *fakeGrantStore
			wantErr string
		}{
			{
				name:    "parse flags",
				args:    []string{"--unknown"},
				stdin:   strings.NewReader("[]"),
				wantErr: "parse flags:",
			},
			{
				name:    "missing storage path",
				stdin:   strings.NewReader("[]"),
				wantErr: "--db-path or --global-data-dir is required",
			},
			{
				name:    "read stdin",
				args:    []string{"--db-path", "/tmp/wikid.db"},
				stdin:   grantWorkspaceErrorReader{err: errors.New("read failed")},
				wantErr: "read grants input: read failed",
			},
			{
				name:    "decode JSON",
				args:    []string{"--db-path", "/tmp/wikid.db"},
				stdin:   strings.NewReader("{"),
				wantErr: "decode grants input:",
			},
			{
				name:    "upsert",
				args:    []string{"--db-path", "/tmp/wikid.db"},
				stdin:   strings.NewReader(`[{"subject":"frontd","workspaceId":"home","role":"admin"}]`),
				store:   &fakeGrantStore{err: errors.New("upsert failed")},
				wantErr: "upsert grant frontd home: upsert failed",
			},
		}

		for _, tc := range cases {
			tc := tc
			ginkgo.By(tc.name)
			restore := restoreGrantWorkspaceSeams()
			func() {
				defer restore()
				var stderr bytes.Buffer
				if tc.store != nil {
					newGrantStore = func(string) grantStore {
						return tc.store
					}
				}

				code := runGrantWorkspaces(tc.args, tc.stdin, &stderr)

				Expect(code).To(Equal(1))
				Expect(stderr.String()).To(ContainSubstring(tc.wantErr))
			}()
		}
	})
})

type fakeGrantStore struct {
	grants []wikid.Grant
	err    error
}

func (s *fakeGrantStore) Upsert(grant wikid.Grant) error {
	if s.err != nil {
		return s.err
	}
	s.grants = append(s.grants, grant)
	return nil
}

func restoreGrantWorkspaceSeams() func() {
	previousArgs := grantWorkspaceArgs
	previousStdin := grantWorkspaceStdin
	previousStderr := grantWorkspaceStderr
	previousExit := grantWorkspaceExit
	previousGrantStore := newGrantStore
	return func() {
		grantWorkspaceArgs = previousArgs
		grantWorkspaceStdin = previousStdin
		grantWorkspaceStderr = previousStderr
		grantWorkspaceExit = previousExit
		newGrantStore = previousGrantStore
	}
}
