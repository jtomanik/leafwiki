package projectdaemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("project daemon deterministic edges", func() {
	ginkgo.It("handles control client and control error parsing edge cases", ginkgo.Label("integration"), func() {
		Expect(&ControlHTTPError{StatusCode: http.StatusTeapot}).To(haveControlStatus(http.StatusTeapot))
		Expect(parsedControlErrorBodyForSpec([]byte(`{"error":{"code":"daemon_control_unauthorized","messageId":"errors.daemon.control_unauthorized","message":" unauthorized "}}`))).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Code":      Equal(errCodeDaemonControlUnauthorized),
			"MessageID": Equal(sharederrors.MessageIDForCode(errCodeDaemonControlUnauthorized)),
		}))
		Expect(parsedControlErrorBodyForSpec([]byte(" plain text "))).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Code":      BeEmpty(),
			"MessageID": BeEmpty(),
		}))

		var seenControlToken string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seenControlToken = req.Header.Get(ControlTokenHeader)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(DaemonHealth{OK: false})
		}))
		ginkgo.DeferCleanup(server.Close)

		client := NewClient(server.URL, "control-token")
		_, err := client.Health(context.Background())
		Expect(err).To(MatchError(errDaemonHealthCheckFailed))
		Expect(seenControlToken).To(Equal("control-token"))

		badURLClient := NewClient("http://127.0.0.1:1/%zz", "control-token")
		err = badURLClient.Ping(context.Background())
		Expect(err).To(matchProjectdaemonURLParseError())

		failingClient := NewClient("http://127.0.0.1", "control-token")
		transportErr := errors.New("projectdaemon transport failed")
		failingClient.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})}
		err = failingClient.ReleaseSession(context.Background(), "session")
		Expect(err).To(MatchError(transportErr))

		noBodyClient := NewClient(server.URL, "control-token")
		err = noBodyClient.doJSON(context.Background(), http.MethodGet, "/health", nil, nil)
		Expect(err).To(Succeed())

		var _ workspaceid.WorkspaceID = "home"
	})

	ginkgo.It("surfaces actor-context and config failure paths", ginkgo.Label("unit"), func() {
		marshalActorContextErr := errors.New("actor context marshal failed")
		originalMarshalActorContext := marshalActorContextJSON
		marshalActorContextJSON = func(any) ([]byte, error) {
			return nil, marshalActorContextErr
		}
		_, err := EncodeActorContext(ActorContext{})
		Expect(err).To(MatchError(marshalActorContextErr))
		marshalActorContextJSON = originalMarshalActorContext

		wire := actorContextWire{
			Version:     1,
			Issuer:      ActorContextIssuerWikid,
			Subject:     "user:admin",
			WorkspaceID: "not valid",
			ExpiresAt:   time.Now().Add(time.Minute),
		}
		raw, err := json.Marshal(wire)
		Expect(err).ToNot(HaveOccurred())
		_, err = DecodeActorContext(base64.RawURLEncoding.EncodeToString(raw), ActorContextValidation{})
		Expect(err).To(matchWorkspaceIDValidationError(workspaceid.ErrCodeWorkspaceIDInvalid))

		Expect(validateActorContext(ActorContext{
			Version:     1,
			Issuer:      ActorContextIssuerWikid,
			Subject:     "user:admin",
			WorkspaceID: "home",
			ExpiresAt:   time.Now().Add(time.Minute),
		}, ActorContextValidation{})).To(Succeed())

		marshalConfigErr := errors.New("config marshal failed")
		originalMarshalConfig := marshalConfigJSON
		marshalConfigJSON = func(any) ([]byte, error) {
			return nil, marshalConfigErr
		}
		_, err = ConfigHash(Config{})
		Expect(err).To(MatchError(marshalConfigErr))
		marshalConfigJSON = originalMarshalConfig

		readRandomErr := errors.New("random failed")
		originalReadRandom := readRandom
		readRandom = func([]byte) (int, error) {
			return 0, readRandomErr
		}
		_, err = RandomToken()
		Expect(err).To(MatchError(readRandomErr))
		_, err = randomID()
		Expect(err).To(MatchError(readRandomErr))
		_, err = NewSessionRegistry(time.Minute, nil).Register()
		Expect(err).To(MatchError(readRandomErr))
		readRandom = originalReadRandom
	})

	ginkgo.It("reports canonical path resolution errors deterministically", ginkgo.Label("unit"), func() {
		originalAbsPath := absPath
		originalEvalSymlinks := evalSymlinks
		ginkgo.DeferCleanup(func() {
			absPath = originalAbsPath
			evalSymlinks = originalEvalSymlinks
		})

		absErr := errors.New("projectdaemon data path failed")
		absPath = func(string) (string, error) {
			return "", absErr
		}
		_, _, err := CanonicalizeProject("data", "root")
		Expect(err).To(MatchError(absErr))

		calls := 0
		rootAbsErr := errors.New("projectdaemon root path failed")
		absPath = func(path string) (string, error) {
			calls++
			if calls == 2 {
				return "", rootAbsErr
			}
			return filepath.Join(string(filepath.Separator), "data"), nil
		}
		evalSymlinks = func(path string) (string, error) {
			return path, nil
		}
		_, _, err = CanonicalizeProject("data", "root")
		Expect(err).To(MatchError(rootAbsErr))

		absPath = func(string) (string, error) {
			return filepath.Join(string(filepath.Separator), "denied"), nil
		}
		evalSymlinks = func(string) (string, error) {
			return "", os.ErrPermission
		}
		_, err = canonicalPath("denied")
		Expect(err).To(MatchError(os.ErrPermission))

		absPath = func(string) (string, error) {
			return filepath.Join(string(filepath.Separator), "missing", "child"), nil
		}
		evalSymlinks = func(path string) (string, error) {
			if filepath.Base(path) == "missing" {
				return "", os.ErrPermission
			}
			return "", os.ErrNotExist
		}
		_, err = canonicalPath("missing/child")
		Expect(err).To(MatchError(os.ErrPermission))

		absPath = func(string) (string, error) {
			return string(filepath.Separator), nil
		}
		evalSymlinks = func(string) (string, error) {
			return "", os.ErrNotExist
		}
		resolved, err := canonicalPath("missing-to-root")
		Expect(err).ToNot(HaveOccurred())
		Expect(resolved).To(Equal(filepath.Clean(string(filepath.Separator))))
	})

	ginkgo.It("reports descriptor atomic-write failure branches", ginkgo.Label("integration"), func() {
		tmp := tempProjectdaemonDir()
		desc := &Descriptor{SchemaVersion: DescriptorSchemaVersion}

		originalMarshalDescriptor := marshalDescriptorJSON
		originalCreateDescriptorTempFile := createDescriptorTempFile
		originalRenameTemporaryDescriptor := renameTemporaryDescriptor
		originalChmodDescriptorFile := chmodDescriptorFile
		ginkgo.DeferCleanup(func() {
			marshalDescriptorJSON = originalMarshalDescriptor
			createDescriptorTempFile = originalCreateDescriptorTempFile
			renameTemporaryDescriptor = originalRenameTemporaryDescriptor
			chmodDescriptorFile = originalChmodDescriptorFile
		})

		marshalErr := errors.New("descriptor marshal failed")
		marshalDescriptorJSON = func(any, string, string) ([]byte, error) {
			return nil, marshalErr
		}
		Expect(WriteDescriptorAtomic(filepath.Join(tmp, "marshal.json"), desc)).To(MatchError(marshalErr))
		marshalDescriptorJSON = originalMarshalDescriptor

		createErr := errors.New("create failed")
		chmodTempErr := errors.New("chmod failed")
		writeErr := errors.New("write failed")
		newlineErr := errors.New("newline failed")
		closeErr := errors.New("close failed")
		renameErr := errors.New("rename failed")
		finalChmodErr := errors.New("final chmod failed")
		cases := []struct {
			name      string
			temp      *fakeDescriptorTempFile
			createErr error
			renameErr error
			chmodErr  error
			wantErr   error
		}{
			{name: "create", createErr: createErr, wantErr: createErr},
			{name: "chmod temp", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "chmod.tmp"), chmodErr: chmodTempErr}, wantErr: chmodTempErr},
			{name: "write body", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "write.tmp"), writeErrs: map[int]error{1: writeErr}}, wantErr: writeErr},
			{name: "write newline", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "newline.tmp"), writeErrs: map[int]error{2: newlineErr}}, wantErr: newlineErr},
			{name: "close", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "close.tmp"), closeErr: closeErr}, wantErr: closeErr},
			{name: "rename", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "rename.tmp")}, renameErr: renameErr, wantErr: renameErr},
			{name: "chmod final", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "final.tmp")}, chmodErr: finalChmodErr, wantErr: finalChmodErr},
		}

		for _, tt := range cases {
			tt := tt
			createDescriptorTempFile = func(string, string) (descriptorTempFile, error) {
				if tt.createErr != nil {
					return nil, tt.createErr
				}
				return tt.temp, nil
			}
			renameTemporaryDescriptor = func(string, string) error {
				return tt.renameErr
			}
			chmodDescriptorFile = func(string, os.FileMode) error {
				return tt.chmodErr
			}

			err := WriteDescriptorAtomic(filepath.Join(tmp, tt.name+".json"), desc)
			Expect(err).To(MatchError(tt.wantErr))
		}
	})
})
