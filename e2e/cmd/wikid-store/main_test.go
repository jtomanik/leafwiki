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

var _ = ginkgo.Describe("wikid-store command", ginkgo.Label("unit"), func() {
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
