package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

const grantWorkspaceCommandFailure = 1

var _ = ginkgo.Describe("grant-workspaces command", ginkgo.Label("unit"), func() {
	ginkgo.It("default seams read process args and create the concrete grant store", func() {
		Expect(grantWorkspaceArgs()).NotTo(BeNil())
		Expect(newGrantStore("/tmp/wikid.db")).To(BeAssignableToTypeOf(wikid.NewGrantStore("")))
	})

	ginkgo.It("main delegates to the runner and exits with its status", func() {
		restore := restoreGrantWorkspaceSeams()
		ginkgo.DeferCleanup(restore)
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

		Expect(exitCode).To(BeZero())
		Expect(stderr.String()).To(BeEmpty())
	})

	ginkgo.It("upserts grants using a db path derived from the global data dir", func() {
		restore := restoreGrantWorkspaceSeams()
		ginkgo.DeferCleanup(restore)
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

		Expect(code).To(BeZero(), stderr.String())
		Expect(storePath).To(Equal(wikid.GlobalLayout("/tmp/global").DBPath))
		Expect(store.grants).To(Equal([]wikid.Grant{{
			Subject:     "frontd",
			WorkspaceID: fixtureGrantWorkspaceID("home"),
			Role:        wikid.GrantRoleAdmin,
		}}))
	})

	ginkgo.DescribeTable("reports semantic failures for invalid inputs and store errors",
		func(tc grantWorkspaceFailureCase) {
			restore := restoreGrantWorkspaceSeams()
			ginkgo.DeferCleanup(restore)
			var stderr bytes.Buffer
			if tc.store != nil {
				newGrantStore = func(string) grantStore {
					return tc.store
				}
			}

			code := runGrantWorkspaces(tc.args, tc.stdin, &stderr)

			Expect(code).To(Equal(grantWorkspaceCommandFailure))
			Expect(grantWorkspaceFailureReportFrom(stderr.String())).To(Equal(tc.wantFailure))
		},
		ginkgo.Entry("reports flag parsing failures", grantWorkspaceFailureCase{
			args:        []string{"--unknown"},
			stdin:       strings.NewReader("[]"),
			wantFailure: grantWorkspaceFailureReport{Kind: grantWorkspaceFailureFlagParsing},
		}),
		ginkgo.Entry("reports missing storage path failures", grantWorkspaceFailureCase{
			stdin:       strings.NewReader("[]"),
			wantFailure: grantWorkspaceFailureReport{Kind: grantWorkspaceFailureMissingStoragePath},
		}),
		ginkgo.Entry("reports unreadable grant input failures", grantWorkspaceFailureCase{
			args:        []string{"--db-path", "/tmp/wikid.db"},
			stdin:       grantWorkspaceErrorReader{err: errors.New("read failed")},
			wantFailure: grantWorkspaceFailureReport{Kind: grantWorkspaceFailureReadInput},
		}),
		ginkgo.Entry("reports malformed grant JSON failures", grantWorkspaceFailureCase{
			args:        []string{"--db-path", "/tmp/wikid.db"},
			stdin:       strings.NewReader("{"),
			wantFailure: grantWorkspaceFailureReport{Kind: grantWorkspaceFailureDecodeInput},
		}),
		ginkgo.Entry("reports failed grant upserts with subject and workspace identity", grantWorkspaceFailureCase{
			args:  []string{"--db-path", "/tmp/wikid.db"},
			stdin: strings.NewReader(`[{"subject":"frontd","workspaceId":"home","role":"admin"}]`),
			store: &fakeGrantStore{err: errors.New("upsert failed")},
			wantFailure: grantWorkspaceFailureReport{
				Kind:        grantWorkspaceFailureUpsert,
				Subject:     "frontd",
				WorkspaceID: fixtureGrantWorkspaceID("home"),
			},
		}),
	)
})

func fixtureGrantWorkspaceID(raw string) workspaceid.WorkspaceID {
	ginkgo.GinkgoHelper()
	id, err := workspaceid.ParseWorkspaceID(raw)
	Expect(err).To(Succeed(), fmt.Sprintf("parse fixture workspace ID %q", raw))
	return id
}

type grantWorkspaceFailureCase struct {
	args        []string
	stdin       io.Reader
	store       *fakeGrantStore
	wantFailure grantWorkspaceFailureReport
}

type grantWorkspaceFailureKind string

const (
	grantWorkspaceFailureUnknown            grantWorkspaceFailureKind = "unknown grant-workspaces failure"
	grantWorkspaceFailureFlagParsing        grantWorkspaceFailureKind = "flag parsing failure"
	grantWorkspaceFailureMissingStoragePath grantWorkspaceFailureKind = "missing storage path failure"
	grantWorkspaceFailureReadInput          grantWorkspaceFailureKind = "grant input read failure"
	grantWorkspaceFailureDecodeInput        grantWorkspaceFailureKind = "grant input decoding failure"
	grantWorkspaceFailureUpsert             grantWorkspaceFailureKind = "grant upsert failure"
)

type grantWorkspaceFailureReport struct {
	Kind        grantWorkspaceFailureKind
	Subject     string
	WorkspaceID workspaceid.WorkspaceID
}

func grantWorkspaceFailureReportFrom(stderr string) grantWorkspaceFailureReport {
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		line = strings.TrimSpace(line)
		if _, ok := strings.CutPrefix(line, "parse flags:"); ok {
			return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureFlagParsing}
		}
		if line == "--db-path or --global-data-dir is required" {
			return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureMissingStoragePath}
		}
		if _, ok := strings.CutPrefix(line, "read grants input:"); ok {
			return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureReadInput}
		}
		if _, ok := strings.CutPrefix(line, "decode grants input:"); ok {
			return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureDecodeInput}
		}
		if _, ok := strings.CutPrefix(line, "upsert grant "); ok {
			return grantWorkspaceUpsertFailureReportFrom(line)
		}
	}
	return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureUnknown}
}

func grantWorkspaceUpsertFailureReportFrom(line string) grantWorkspaceFailureReport {
	rest, ok := strings.CutPrefix(line, "upsert grant ")
	if !ok {
		return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureUnknown}
	}
	subject, rest, ok := strings.Cut(rest, " ")
	if !ok {
		return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureUnknown}
	}
	rawWorkspaceID, _, ok := strings.Cut(rest, ": ")
	if !ok {
		return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureUnknown}
	}
	parsedWorkspaceID, err := workspaceid.ParseWorkspaceID(rawWorkspaceID)
	if err != nil {
		return grantWorkspaceFailureReport{Kind: grantWorkspaceFailureUnknown}
	}
	return grantWorkspaceFailureReport{
		Kind:        grantWorkspaceFailureUpsert,
		Subject:     subject,
		WorkspaceID: parsedWorkspaceID,
	}
}

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
