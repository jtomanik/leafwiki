package main

import (
	"encoding/json"
	"fmt"
	"io"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

const wikidStoreCommandFailure = 1

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
	return wikid.WorkspaceRecord{ID: fixtureWikidStoreWorkspaceID("home"), DisplayName: "Home", DataDir: "/data", RootDir: "/root"}
}

func fixtureWikidStoreWorkspaceID(raw string) workspaceid.WorkspaceID {
	ginkgo.GinkgoHelper()
	id, err := workspaceid.ParseWorkspaceID(raw)
	Expect(err).To(Succeed(), fmt.Sprintf("parse fixture workspace ID %q", raw))
	return id
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
