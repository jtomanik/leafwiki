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

var _ = ginkgo.Describe("wikid-store command", func() {
	ginkgo.It("default seams read process args and create concrete stores", func() {
		Expect(wikidStoreArgs()).NotTo(BeNil())
		Expect(newRegistryStore("/tmp/wikid.db")).To(BeAssignableToTypeOf(wikid.NewRegistryStore("")))
		Expect(newRegistryService("/tmp/wikid.db", wikid.GlobalLayout("/tmp/global"))).To(BeAssignableToTypeOf(wikid.NewRegistryService(wikid.NewRegistryStore(""), wikid.GlobalLayout(""))))
		Expect(newGrantStore("/tmp/wikid.db")).To(BeAssignableToTypeOf(wikid.NewGrantStore("")))
	})

	ginkgo.It("main delegates to the runner and exits with its status", func() {
		restore := restoreWikidStoreSeams()
		ginkgo.DeferCleanup(restore)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		var exitCode int
		wikidStoreArgs = func() []string {
			return []string{"read-registry", "--global-data-dir", "/tmp/global"}
		}
		wikidStoreStdin = strings.NewReader("")
		wikidStoreStdout = &stdout
		wikidStoreStderr = &stderr
		wikidStoreExit = func(code int) {
			exitCode = code
			panic("exit")
		}
		newRegistryStore = func(string) registryStore {
			return &fakeRegistryStore{doc: wikid.NewRegistryDocument()}
		}

		Expect(func() { main() }).To(PanicWith("exit"))

		Expect(exitCode).To(Equal(0), stderr.String())
		Expect(stdout.String()).To(ContainSubstring(`"schemaVersion"`))
	})

	ginkgo.DescribeTable("reads the registry and reports registry or output errors",
		func(tc wikidStoreReadRegistryCase) {
			restore := restoreWikidStoreSeams()
			ginkgo.DeferCleanup(restore)
			var stderr bytes.Buffer
			stdout := tc.stdout()
			stdoutBuffer, _ := stdout.(*bytes.Buffer)
			newRegistryStore = func(string) registryStore {
				return tc.store()
			}

			code := runWikidStore([]string{"read-registry", "--global-data-dir", "/tmp/global"}, strings.NewReader(""), stdout, &stderr)

			Expect(code).To(Equal(tc.wantCode))
			if tc.wantStdout != "" {
				Expect(stdoutBuffer).NotTo(BeNil())
				Expect(stdoutBuffer.String()).To(ContainSubstring(tc.wantStdout))
			}
			if tc.wantStderr != "" {
				Expect(stderr.String()).To(ContainSubstring(tc.wantStderr))
			}
		},
		ginkgo.Entry("success", wikidStoreReadRegistryCase{
			store:      func() registryStore { return &fakeRegistryStore{doc: wikid.NewRegistryDocument()} },
			stdout:     func() io.Writer { return &bytes.Buffer{} },
			wantCode:   0,
			wantStdout: `"schemaVersion"`,
		}),
		ginkgo.Entry("load error", wikidStoreReadRegistryCase{
			store:      func() registryStore { return &fakeRegistryStore{err: errors.New("load failed")} },
			stdout:     func() io.Writer { return &bytes.Buffer{} },
			wantCode:   1,
			wantStderr: "load registry: load failed",
		}),
		ginkgo.Entry("write error", wikidStoreReadRegistryCase{
			store:      func() registryStore { return &fakeRegistryStore{doc: wikid.NewRegistryDocument()} },
			stdout:     func() io.Writer { return errorWriter{err: errors.New("write failed")} },
			wantCode:   1,
			wantStderr: "encode stdout JSON: write failed",
		}),
	)

	ginkgo.DescribeTable("registers workspaces and reports input, service, and output errors",
		func(tc wikidStoreRegisterWorkspaceCase) {
			restore := restoreWikidStoreSeams()
			ginkgo.DeferCleanup(restore)
			var stderr bytes.Buffer
			var servicePath string
			service := tc.service()
			newRegistryService = func(path string, layout wikid.Layout) registryService {
				servicePath = path
				return service
			}

			code := runWikidStore([]string{"register-workspace", "--global-data-dir", "/tmp/global"}, tc.stdin(), tc.stdout(), &stderr)

			Expect(code).To(Equal(tc.wantCode))
			if tc.wantCode == 0 {
				Expect(servicePath).To(Equal(wikid.GlobalLayout("/tmp/global").DBPath))
				Expect(service.request.DisplayName).To(Equal("Docs"))
				Expect(service.request.MarkdownLinkRootPrefix).To(Equal("/docs"))
			}
			if tc.wantStderr != "" {
				Expect(stderr.String()).To(ContainSubstring(tc.wantStderr))
			}
		},
		ginkgo.Entry("success", wikidStoreRegisterWorkspaceCase{
			stdin: func() io.Reader {
				return strings.NewReader(`{"displayName":"Docs","dataDir":"/data","rootDir":"/root","markdownLinkRootPrefix":"/docs"}`)
			},
			service:  func() *fakeRegistryService { return &fakeRegistryService{workspace: fakeWorkspaceRecord()} },
			stdout:   func() io.Writer { return &bytes.Buffer{} },
			wantCode: 0,
		}),
		ginkgo.Entry("read error", wikidStoreRegisterWorkspaceCase{
			stdin:      func() io.Reader { return errorReader{err: errors.New("read failed")} },
			service:    func() *fakeRegistryService { return &fakeRegistryService{workspace: fakeWorkspaceRecord()} },
			stdout:     func() io.Writer { return &bytes.Buffer{} },
			wantCode:   1,
			wantStderr: "read stdin: read failed",
		}),
		ginkgo.Entry("decode error", wikidStoreRegisterWorkspaceCase{
			stdin:      func() io.Reader { return strings.NewReader("{") },
			service:    func() *fakeRegistryService { return &fakeRegistryService{workspace: fakeWorkspaceRecord()} },
			stdout:     func() io.Writer { return &bytes.Buffer{} },
			wantCode:   1,
			wantStderr: "decode stdin JSON:",
		}),
		ginkgo.Entry("service error", wikidStoreRegisterWorkspaceCase{
			stdin:      func() io.Reader { return strings.NewReader(`{"displayName":"Docs"}`) },
			service:    func() *fakeRegistryService { return &fakeRegistryService{err: errors.New("register failed")} },
			stdout:     func() io.Writer { return &bytes.Buffer{} },
			wantCode:   1,
			wantStderr: "register workspace: register failed",
		}),
		ginkgo.Entry("write error", wikidStoreRegisterWorkspaceCase{
			stdin:      func() io.Reader { return strings.NewReader(`{"displayName":"Docs"}`) },
			service:    func() *fakeRegistryService { return &fakeRegistryService{workspace: fakeWorkspaceRecord()} },
			stdout:     func() io.Writer { return errorWriter{err: errors.New("write failed")} },
			wantCode:   1,
			wantStderr: "encode stdout JSON: write failed",
		}),
	)

	ginkgo.It("upserts grants and reports grant input or store errors", func() {
		store := &fakeWikidGrantStore{}
		restore := restoreWikidStoreSeams()
		ginkgo.DeferCleanup(restore)
		newGrantStore = func(string) grantStore {
			return store
		}
		var stderr bytes.Buffer

		code := runWikidStore(
			[]string{"upsert-grants", "--global-data-dir", "/tmp/global"},
			strings.NewReader(`[{"subject":"frontd","workspaceId":"home","role":"admin"}]`),
			io.Discard,
			&stderr,
		)

		Expect(code).To(Equal(0), stderr.String())
		Expect(store.upserts).To(Equal([]wikid.Grant{{
			Subject:     "frontd",
			WorkspaceID: workspaceid.WorkspaceID("home"),
			Role:        wikid.GrantRole("admin"),
		}}))

		store.upsertErr = errors.New("upsert failed")
		stderr.Reset()
		code = runWikidStore(
			[]string{"upsert-grants", "--global-data-dir", "/tmp/global"},
			strings.NewReader(`[{"subject":"frontd","workspaceId":"home","role":"admin"}]`),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(stderr.String()).To(ContainSubstring("upsert grant frontd home: upsert failed"))

		stderr.Reset()
		code = runWikidStore(
			[]string{"upsert-grants", "--global-data-dir", "/tmp/global"},
			strings.NewReader("{"),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(stderr.String()).To(ContainSubstring("decode stdin JSON:"))
	})

	ginkgo.It("replaces subject grants and reports missing subjects or store errors", func() {
		store := &fakeWikidGrantStore{}
		restore := restoreWikidStoreSeams()
		ginkgo.DeferCleanup(restore)
		newGrantStore = func(string) grantStore {
			return store
		}
		var stderr bytes.Buffer

		code := runWikidStore(
			[]string{"replace-subject-grants", "--global-data-dir", "/tmp/global"},
			strings.NewReader("[]"),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(stderr.String()).To(ContainSubstring("--subject is required"))

		stderr.Reset()
		code = runWikidStore(
			[]string{"replace-subject-grants", "--global-data-dir", "/tmp/global", "--subject", "frontd"},
			strings.NewReader(`[{"workspaceId":"home","role":"admin"}]`),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(0), stderr.String())
		Expect(store.replaceSubject).To(Equal("frontd"))
		Expect(store.replaceGrants).To(HaveLen(1))

		stderr.Reset()
		code = runWikidStore(
			[]string{"replace-subject-grants", "--global-data-dir", "/tmp/global", "--subject", "frontd"},
			strings.NewReader("{"),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(stderr.String()).To(ContainSubstring("decode stdin JSON:"))

		store.replaceErr = errors.New("replace failed")
		stderr.Reset()
		code = runWikidStore(
			[]string{"replace-subject-grants", "--global-data-dir", "/tmp/global", "--subject", "frontd"},
			strings.NewReader(`[]`),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(stderr.String()).To(ContainSubstring("replace grants for frontd: replace failed"))
	})

	ginkgo.DescribeTable("reports command line validation errors",
		func(args []string, wantErr string) {
			var stderr bytes.Buffer

			code := runWikidStore(args, strings.NewReader(""), io.Discard, &stderr)

			Expect(code).To(Equal(1))
			Expect(stderr.String()).To(ContainSubstring(wantErr))
		},
		ginkgo.Entry("usage", nil, "usage: wikid-store"),
		ginkgo.Entry("parse", []string{"read-registry", "--unknown"}, "parse flags:"),
		ginkgo.Entry("global data dir", []string{"read-registry"}, "--global-data-dir is required"),
		ginkgo.Entry("unknown command", []string{"nope", "--global-data-dir", "/tmp/global"}, `unknown command "nope"`),
	)
})

type wikidStoreReadRegistryCase struct {
	store      func() registryStore
	stdout     func() io.Writer
	wantCode   int
	wantStdout string
	wantStderr string
}

type wikidStoreRegisterWorkspaceCase struct {
	stdin      func() io.Reader
	service    func() *fakeRegistryService
	stdout     func() io.Writer
	wantCode   int
	wantStderr string
}

func fakeWorkspaceRecord() wikid.WorkspaceRecord {
	return wikid.WorkspaceRecord{ID: workspaceid.WorkspaceID("home"), DisplayName: "Home", DataDir: "/data", RootDir: "/root"}
}

type fakeRegistryStore struct {
	doc wikid.RegistryDocument
	err error
}

func (s *fakeRegistryStore) Load() (wikid.RegistryDocument, error) {
	if s.err != nil {
		return wikid.RegistryDocument{}, s.err
	}
	return s.doc, nil
}

type fakeRegistryService struct {
	workspace wikid.WorkspaceRecord
	request   wikid.RegisterWorkspaceRequest
	err       error
}

func (s *fakeRegistryService) RegisterWorkspace(req wikid.RegisterWorkspaceRequest) (wikid.WorkspaceRecord, error) {
	s.request = req
	if s.err != nil {
		return wikid.WorkspaceRecord{}, s.err
	}
	return s.workspace, nil
}

type fakeWikidGrantStore struct {
	upserts        []wikid.Grant
	upsertErr      error
	replaceSubject string
	replaceGrants  []wikid.Grant
	replaceErr     error
}

func (s *fakeWikidGrantStore) Upsert(grant wikid.Grant) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserts = append(s.upserts, grant)
	return nil
}

func (s *fakeWikidGrantStore) ReplaceSubjectGrants(subject string, grants []wikid.Grant) error {
	if s.replaceErr != nil {
		return s.replaceErr
	}
	s.replaceSubject = subject
	s.replaceGrants = append([]wikid.Grant(nil), grants...)
	return nil
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func restoreWikidStoreSeams() func() {
	previousArgs := wikidStoreArgs
	previousStdin := wikidStoreStdin
	previousStdout := wikidStoreStdout
	previousStderr := wikidStoreStderr
	previousExit := wikidStoreExit
	previousRegistryStore := newRegistryStore
	previousRegistryService := newRegistryService
	previousGrantStore := newGrantStore
	return func() {
		wikidStoreArgs = previousArgs
		wikidStoreStdin = previousStdin
		wikidStoreStdout = previousStdout
		wikidStoreStderr = previousStderr
		wikidStoreExit = previousExit
		newRegistryStore = previousRegistryStore
		newRegistryService = previousRegistryService
		newGrantStore = previousGrantStore
	}
}
