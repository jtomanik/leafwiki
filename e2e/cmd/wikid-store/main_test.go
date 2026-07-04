package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

const (
	wikidStoreSchemaVersionJSON             = `"schemaVersion"`
	wikidStoreFixtureDisplayName            = "Docs"
	wikidStoreFixtureMarkdownLinkRootPrefix = "/docs"
)

var (
	errWikidStoreLoadFailed     = errors.New("load failed")
	errWikidStoreWriteFailed    = errors.New("write failed")
	errWikidStoreReadFailed     = errors.New("read failed")
	errWikidStoreRegisterFailed = errors.New("register failed")
	errWikidStoreUpsertFailed   = errors.New("upsert failed")
	errWikidStoreReplaceFailed  = errors.New("replace failed")
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

		Expect(exitCode).To(BeZero(), stderr.String())
		Expect(stdout.String()).To(haveWikidStoreSchemaVersionJSON())
	})

	ginkgo.DescribeTable("reads the registry and reports registry or output errors",
		func(tc wikidStoreReadRegistryCase) {
			restore := restoreWikidStoreSeams()
			ginkgo.DeferCleanup(restore)
			var stderr bytes.Buffer
			store := tc.store()
			stdout := tc.stdout()
			stdoutBuffer, _ := stdout.(*bytes.Buffer)
			newRegistryStore = func(string) registryStore {
				return store
			}

			code := runWikidStore([]string{"read-registry", "--global-data-dir", "/tmp/global"}, strings.NewReader(""), stdout, &stderr)

			Expect(code).To(Equal(tc.wantCode))
			Expect(store).To(haveWikidStoreRegistryLoadAttempts(1))
			if tc.wantStdout != nil {
				Expect(stdoutBuffer).NotTo(BeNil())
				Expect(stdoutBuffer.String()).To(tc.wantStdout)
			}
			if tc.wantWriter != nil {
				Expect(stdout).To(tc.wantWriter)
			}
		},
		ginkgo.Entry("success", wikidStoreReadRegistryCase{
			store:      func() *fakeRegistryStore { return &fakeRegistryStore{doc: wikid.NewRegistryDocument()} },
			stdout:     func() io.Writer { return &bytes.Buffer{} },
			wantCode:   0,
			wantStdout: haveWikidStoreSchemaVersionJSON(),
		}),
		ginkgo.Entry("load error", wikidStoreReadRegistryCase{
			store:    func() *fakeRegistryStore { return &fakeRegistryStore{err: errWikidStoreLoadFailed} },
			stdout:   func() io.Writer { return &bytes.Buffer{} },
			wantCode: 1,
		}),
		ginkgo.Entry("write error", wikidStoreReadRegistryCase{
			store:      func() *fakeRegistryStore { return &fakeRegistryStore{doc: wikid.NewRegistryDocument()} },
			stdout:     func() io.Writer { return &errorWriter{err: errWikidStoreWriteFailed} },
			wantCode:   1,
			wantWriter: haveWikidStoreWriteAttempts(1),
		}),
	)

	ginkgo.DescribeTable("registers workspaces and reports input, service, and output errors",
		func(tc wikidStoreRegisterWorkspaceCase) {
			restore := restoreWikidStoreSeams()
			ginkgo.DeferCleanup(restore)
			var stderr bytes.Buffer
			var servicePath string
			service := tc.service()
			stdout := tc.stdout()
			newRegistryService = func(path string, layout wikid.Layout) registryService {
				servicePath = path
				return service
			}

			code := runWikidStore([]string{"register-workspace", "--global-data-dir", "/tmp/global"}, tc.stdin(), stdout, &stderr)

			Expect(code).To(Equal(tc.wantCode))
			Expect(wikidStoreRegistryServiceSnapshot{
				Path:          servicePath,
				RegisterCalls: service.registerCalls,
				Request:       service.request,
			}).To(tc.wantService)
			if tc.wantWriter != nil {
				Expect(stdout).To(tc.wantWriter)
			}
		},
		ginkgo.Entry("success", wikidStoreRegisterWorkspaceCase{
			stdin: func() io.Reader {
				return strings.NewReader(`{"displayName":"Docs","dataDir":"/data","rootDir":"/root","markdownLinkRootPrefix":"/docs"}`)
			},
			service:  func() *fakeRegistryService { return &fakeRegistryService{workspace: fakeWorkspaceRecord()} },
			stdout:   func() io.Writer { return &bytes.Buffer{} },
			wantCode: 0,
			wantService: haveWikidStoreRegistryServiceRequest(
				matchWikidStoreRegisterWorkspaceRequest(wikidStoreFixtureMarkdownLinkRootPrefix),
			),
		}),
		ginkgo.Entry("read error", wikidStoreRegisterWorkspaceCase{
			stdin:       func() io.Reader { return errorReader{err: errWikidStoreReadFailed} },
			service:     func() *fakeRegistryService { return &fakeRegistryService{workspace: fakeWorkspaceRecord()} },
			stdout:      func() io.Writer { return &bytes.Buffer{} },
			wantCode:    1,
			wantService: haveNoWikidStoreRegistryServiceRequest(),
		}),
		ginkgo.Entry("decode error", wikidStoreRegisterWorkspaceCase{
			stdin:       func() io.Reader { return strings.NewReader("{") },
			service:     func() *fakeRegistryService { return &fakeRegistryService{workspace: fakeWorkspaceRecord()} },
			stdout:      func() io.Writer { return &bytes.Buffer{} },
			wantCode:    1,
			wantService: haveNoWikidStoreRegistryServiceRequest(),
		}),
		ginkgo.Entry("service error", wikidStoreRegisterWorkspaceCase{
			stdin:    func() io.Reader { return strings.NewReader(`{"displayName":"Docs"}`) },
			service:  func() *fakeRegistryService { return &fakeRegistryService{err: errWikidStoreRegisterFailed} },
			stdout:   func() io.Writer { return &bytes.Buffer{} },
			wantCode: 1,
			wantService: haveWikidStoreRegistryServiceRequest(
				matchWikidStoreRegisterWorkspaceRequest(""),
			),
		}),
		ginkgo.Entry("write error", wikidStoreRegisterWorkspaceCase{
			stdin:   func() io.Reader { return strings.NewReader(`{"displayName":"Docs"}`) },
			service: func() *fakeRegistryService { return &fakeRegistryService{workspace: fakeWorkspaceRecord()} },
			stdout: func() io.Writer {
				return &errorWriter{err: errWikidStoreWriteFailed}
			},
			wantCode: 1,
			wantService: haveWikidStoreRegistryServiceRequest(
				matchWikidStoreRegisterWorkspaceRequest(""),
			),
			wantWriter: haveWikidStoreWriteAttempts(1),
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

		Expect(code).To(BeZero(), stderr.String())
		Expect(store).To(haveWikidStoreGrantUpserts(
			Equal([]wikid.Grant{{
				Subject:     "frontd",
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}}),
			Equal([]wikid.Grant{{
				Subject:     "frontd",
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}}),
		))

		store.upsertErr = errWikidStoreUpsertFailed
		stderr.Reset()
		code = runWikidStore(
			[]string{"upsert-grants", "--global-data-dir", "/tmp/global"},
			strings.NewReader(`[{"subject":"frontd","workspaceId":"home","role":"admin"}]`),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(store).To(haveWikidStoreGrantUpserts(
			Equal([]wikid.Grant{{
				Subject:     "frontd",
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}, {
				Subject:     "frontd",
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}}),
			Equal([]wikid.Grant{{
				Subject:     "frontd",
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}}),
		))

		stderr.Reset()
		code = runWikidStore(
			[]string{"upsert-grants", "--global-data-dir", "/tmp/global"},
			strings.NewReader("{"),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(store).To(haveWikidStoreGrantUpserts(
			Equal([]wikid.Grant{{
				Subject:     "frontd",
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}, {
				Subject:     "frontd",
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}}),
			Equal([]wikid.Grant{{
				Subject:     "frontd",
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}}),
		))
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
		Expect(store).To(haveRecordedWikidStoreReplace("", BeNil()))
		Expect(store).To(haveWikidStoreReplaceAttempts(BeEmpty()))

		stderr.Reset()
		code = runWikidStore(
			[]string{"replace-subject-grants", "--global-data-dir", "/tmp/global", "--subject", "frontd"},
			strings.NewReader(`[{"workspaceId":"home","role":"admin"}]`),
			io.Discard,
			&stderr,
		)
		Expect(code).To(BeZero(), stderr.String())
		Expect(store).To(haveRecordedWikidStoreReplace("frontd", HaveLen(1)))
		Expect(store).To(haveWikidStoreReplaceAttempts(Equal([]wikidStoreReplaceSnapshot{{
			Subject: "frontd",
			Grants: []wikid.Grant{{
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}},
		}})))

		stderr.Reset()
		code = runWikidStore(
			[]string{"replace-subject-grants", "--global-data-dir", "/tmp/global", "--subject", "frontd"},
			strings.NewReader("{"),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(store).To(haveRecordedWikidStoreReplace("frontd", HaveLen(1)))
		Expect(store).To(haveWikidStoreReplaceAttempts(Equal([]wikidStoreReplaceSnapshot{{
			Subject: "frontd",
			Grants: []wikid.Grant{{
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}},
		}})))

		store.replaceErr = errWikidStoreReplaceFailed
		stderr.Reset()
		code = runWikidStore(
			[]string{"replace-subject-grants", "--global-data-dir", "/tmp/global", "--subject", "frontd"},
			strings.NewReader(`[]`),
			io.Discard,
			&stderr,
		)
		Expect(code).To(Equal(1))
		Expect(store).To(haveRecordedWikidStoreReplace("frontd", HaveLen(1)))
		Expect(store).To(haveWikidStoreReplaceAttempts(Equal([]wikidStoreReplaceSnapshot{{
			Subject: "frontd",
			Grants: []wikid.Grant{{
				WorkspaceID: workspaceid.WorkspaceID("home"),
				Role:        wikid.GrantRoleAdmin,
			}},
		}, {
			Subject: "frontd",
			Grants:  nil,
		}})))
	})

	ginkgo.DescribeTable("rejects invalid command lines with a failure status",
		func(args []string) {
			var stderr bytes.Buffer

			code := runWikidStore(args, strings.NewReader(""), io.Discard, &stderr)

			Expect(code).To(Equal(1))
		},
		ginkgo.Entry("requires a command", nil),
		ginkgo.Entry("rejects unknown read-registry flags", []string{"read-registry", "--unknown"}),
		ginkgo.Entry("requires the global data directory", []string{"read-registry"}),
		ginkgo.Entry("rejects unknown commands", []string{"nope", "--global-data-dir", "/tmp/global"}),
	)
})

type wikidStoreReadRegistryCase struct {
	store      func() *fakeRegistryStore
	stdout     func() io.Writer
	wantCode   int
	wantStdout types.GomegaMatcher
	wantWriter types.GomegaMatcher
}

type wikidStoreRegisterWorkspaceCase struct {
	stdin       func() io.Reader
	service     func() *fakeRegistryService
	stdout      func() io.Writer
	wantCode    int
	wantService types.GomegaMatcher
	wantWriter  types.GomegaMatcher
}

type wikidStoreReplaceSnapshot struct {
	Subject string
	Grants  []wikid.Grant
}

type wikidStoreRegistryServiceSnapshot struct {
	Path          string
	RegisterCalls int
	Request       wikid.RegisterWorkspaceRequest
}

type wikidStoreGrantUpsertSnapshot struct {
	Attempts []wikid.Grant
	Stored   []wikid.Grant
}

func haveWikidStoreSchemaVersionJSON() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(raw string) map[string]any {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil
		}
		return payload
	}, HaveKey("schemaVersion"))
}

func haveWikidStoreRegistryLoadAttempts(count int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(store *fakeRegistryStore) int {
		return store.loadCalls
	}, Equal(count))
}

func haveWikidStoreWriteAttempts(count int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(writer *errorWriter) int {
		return writer.writeCalls
	}, Equal(count))
}

func haveNoWikidStoreRegistryServiceRequest() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Path":          BeEmpty(),
		"RegisterCalls": BeZero(),
	})
}

func haveWikidStoreRegistryServiceRequest(request types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Path":          Equal(wikid.GlobalLayout("/tmp/global").DBPath),
		"RegisterCalls": Equal(1),
		"Request":       request,
	})
}

func matchWikidStoreRegisterWorkspaceRequest(markdownLinkRootPrefix string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"DisplayName":            Equal(wikidStoreFixtureDisplayName),
		"MarkdownLinkRootPrefix": Equal(markdownLinkRootPrefix),
	})
}

func haveWikidStoreGrantUpserts(attempts types.GomegaMatcher, stored types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(store *fakeWikidGrantStore) wikidStoreGrantUpsertSnapshot {
		return wikidStoreGrantUpsertSnapshot{
			Attempts: store.upsertAttempts,
			Stored:   store.upserts,
		}
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Attempts": attempts,
		"Stored":   stored,
	}))
}

func haveRecordedWikidStoreReplace(subject string, grants types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(store *fakeWikidGrantStore) wikidStoreReplaceSnapshot {
		return wikidStoreReplaceSnapshot{
			Subject: store.replaceSubject,
			Grants:  store.replaceGrants,
		}
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Subject": Equal(subject),
		"Grants":  grants,
	}))
}

func haveWikidStoreReplaceAttempts(attempts types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(store *fakeWikidGrantStore) []wikidStoreReplaceSnapshot {
		return store.replaceAttempts
	}, attempts)
}

func fakeWorkspaceRecord() wikid.WorkspaceRecord {
	return wikid.WorkspaceRecord{ID: workspaceid.WorkspaceID("home"), DisplayName: "Home", DataDir: "/data", RootDir: "/root"}
}

type fakeRegistryStore struct {
	doc       wikid.RegistryDocument
	err       error
	loadCalls int
}

func (s *fakeRegistryStore) Load() (wikid.RegistryDocument, error) {
	s.loadCalls++
	if s.err != nil {
		return wikid.RegistryDocument{}, s.err
	}
	return s.doc, nil
}

type fakeRegistryService struct {
	workspace     wikid.WorkspaceRecord
	request       wikid.RegisterWorkspaceRequest
	err           error
	registerCalls int
}

func (s *fakeRegistryService) RegisterWorkspace(req wikid.RegisterWorkspaceRequest) (wikid.WorkspaceRecord, error) {
	s.registerCalls++
	s.request = req
	if s.err != nil {
		return wikid.WorkspaceRecord{}, s.err
	}
	return s.workspace, nil
}

type fakeWikidGrantStore struct {
	upsertAttempts  []wikid.Grant
	upserts         []wikid.Grant
	upsertErr       error
	replaceSubject  string
	replaceGrants   []wikid.Grant
	replaceAttempts []wikidStoreReplaceSnapshot
	replaceErr      error
}

func (s *fakeWikidGrantStore) Upsert(grant wikid.Grant) error {
	s.upsertAttempts = append(s.upsertAttempts, grant)
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserts = append(s.upserts, grant)
	return nil
}

func (s *fakeWikidGrantStore) ReplaceSubjectGrants(subject string, grants []wikid.Grant) error {
	s.replaceAttempts = append(s.replaceAttempts, wikidStoreReplaceSnapshot{
		Subject: subject,
		Grants:  append([]wikid.Grant(nil), grants...),
	})
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
	err        error
	writeCalls int
}

func (w *errorWriter) Write([]byte) (int, error) {
	w.writeCalls++
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
