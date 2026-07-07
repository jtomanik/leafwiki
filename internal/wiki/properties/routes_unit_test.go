package properties

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	coreprop "github.com/perber/wiki/internal/properties"
)

var _ = ginkgo.Describe("properties route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs routes with configured use cases", func() {
		keys := &GetPropertyKeysUseCase{}
		pages := &GetPagesByPropertyUseCase{}

		routes := NewRoutes(RoutesConfig{GetPropertyKeys: keys, GetPagesByProperty: pages})

		Expect(routes).To(matchPropertyRouteUseCases(propertyRouteUseCases{
			PropertyKeys:    keys,
			PagesByProperty: pages,
		}))
	})

	ginkgo.It("registers public property endpoints without auth middleware", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		(&Routes{}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{PublicAccess: true},
		})

		Expect(propertyRegisteredRoutes(engine)).To(exposePropertyRouteContract())
	})

	ginkgo.It("registers private property endpoints behind auth middleware", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		(&Routes{}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{AuthDisabled: true},
		})

		Expect(propertyRegisteredRoutes(engine)).To(exposePropertyRouteContract())
	})

	ginkgo.It("writes property keys from normalized query parameters", func() {
		ctx, rec := newPropertiesUnitContext("/api/properties?q=status&limit=2")
		keys := &recordingPropertyKeysExecutor{
			out: &GetPropertyKeysOutput{Keys: []coreprop.PropertyKeyCount{{Key: "status", Count: 3}}},
		}

		(&Routes{getPropertyKeys: keys}).handleGetPropertyKeys(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodePropertyKeysResponse(rec)).To(Equal([]coreprop.PropertyKeyCount{{Key: "status", Count: 3}}))
		Expect(keys).To(recordPropertyKeyQuery("status", coreprop.PropertyKeyLimit(2)))
	})

	ginkgo.It("writes structured errors for invalid key limits and key listing failures", func() {
		invalidLimitCtx, invalidLimitRec := newPropertiesUnitContext("/api/properties?limit=bad")
		(&Routes{getPropertyKeys: &recordingPropertyKeysExecutor{}}).handleGetPropertyKeys(invalidLimitCtx)
		Expect(invalidLimitRec).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesInvalidLimit), invalidLimitRec.Body.String())

		failedListingCtx, failedListingRec := newPropertiesUnitContext("/api/properties")
		(&Routes{getPropertyKeys: &recordingPropertyKeysExecutor{err: errors.New("property keys failed")}}).handleGetPropertyKeys(failedListingCtx)
		Expect(failedListingRec).To(matchPropertiesStructuredError(http.StatusInternalServerError, ErrCodePropertiesInternal), failedListingRec.Body.String())
	})

	ginkgo.It("writes property pages from key and value query parameters", func() {
		ctx, rec := newPropertiesUnitContext("/api/properties/pages?key=status&value=draft")
		pages := &recordingPagesByPropertyExecutor{
			out: &GetPagesByPropertyOutput{Pages: []*dto.PropertyPage{{
				ID:    newFixturePageID("page-1").MetadataValue(),
				Title: "Draft",
				Path:  "draft",
			}}},
		}

		(&Routes{getPagesByProperty: pages}).handleGetPagesByProperty(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodePropertyPagesResponse(rec)).To(ConsistOf(matchPropertyPage(
			newFixturePageID("page-1"),
			"Draft",
			"draft",
			BeEmpty(),
		)))
		Expect(pages).To(recordPagesByPropertyQuery("status", "draft"))
	})

	ginkgo.It("writes structured errors for property page lookup failures", func() {
		ctx, rec := newPropertiesUnitContext("/api/properties/pages?key=status")

		(&Routes{getPagesByProperty: &recordingPagesByPropertyExecutor{err: ErrPropertiesMissingValue}}).handleGetPagesByProperty(ctx)

		Expect(rec).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesMissingValue), rec.Body.String())
	})
})

var _ = ginkgo.Describe("properties error responses", ginkgo.Label("unit"), func() {
	ginkgo.It("writes localized and internal structured property errors", func() {
		localized := newPropertiesErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithPropertiesError(ctx, ErrPropertiesMissingKey)
		})
		Expect(localized).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesMissingKey), localized.Body.String())

		internal := newPropertiesErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithPropertiesError(ctx, errors.New("properties store failed"))
		})
		Expect(internal).To(matchPropertiesStructuredError(http.StatusInternalServerError, ErrCodePropertiesInternal), internal.Body.String())
	})

	ginkgo.It("writes localized bad-request property errors", func() {
		rec := newPropertiesErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithPropertiesBadRequest(ctx, ErrCodePropertiesInvalidLimit, "ignored", "ignored")
		})

		Expect(rec).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesInvalidLimit), rec.Body.String())
	})
})

type recordingPropertyKeysExecutor struct {
	out *GetPropertyKeysOutput
	err error

	seen GetPropertyKeysInput
}

func (exec *recordingPropertyKeysExecutor) Execute(_ context.Context, in GetPropertyKeysInput) (*GetPropertyKeysOutput, error) {
	exec.seen = in
	if exec.err != nil {
		return nil, exec.err
	}
	return exec.out, nil
}

type recordingPagesByPropertyExecutor struct {
	out *GetPagesByPropertyOutput
	err error

	seen GetPagesByPropertyInput
}

func (exec *recordingPagesByPropertyExecutor) Execute(_ context.Context, in GetPagesByPropertyInput) (*GetPagesByPropertyOutput, error) {
	exec.seen = in
	if exec.err != nil {
		return nil, exec.err
	}
	return exec.out, nil
}

func newPropertiesUnitContext(target string) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return ctx, rec
}

func propertyRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func decodePropertyKeysResponse(rec *httptest.ResponseRecorder) []coreprop.PropertyKeyCount {
	ginkgo.GinkgoHelper()

	var body []coreprop.PropertyKeyCount
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return body
}

func decodePropertyPagesResponse(rec *httptest.ResponseRecorder) []dto.PropertyPage {
	ginkgo.GinkgoHelper()

	var body []dto.PropertyPage
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return body
}

func newPropertiesErrorResponseRecorder(write func(*gin.Context)) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	write(ctx)
	return rec
}

func recordPropertyKeyQuery(filter string, limit coreprop.PropertyKeyLimit) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedPropertyKeyQuery, Equal(propertyKeyQueryObservation{
		Filter:   filter,
		PageSize: limit,
	}))
}

func recordPagesByPropertyQuery(key string, value string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedPagesByPropertyQuery, Equal(pagesByPropertyQueryObservation{
		Key:   key,
		Value: value,
	}))
}

func exposePropertyRouteContract() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return ConsistOf(
		"GET /api/properties",
		"GET /api/properties/pages",
	)
}

type propertyKeyQueryObservation struct {
	Filter   string
	PageSize coreprop.PropertyKeyLimit
}

func observedPropertyKeyQuery(exec *recordingPropertyKeysExecutor) propertyKeyQueryObservation {
	return propertyKeyQueryObservation{
		Filter:   exec.seen.Filter,
		PageSize: exec.seen.PageSize,
	}
}

type propertyRouteUseCases struct {
	PropertyKeys    propertyKeysExecutor
	PagesByProperty pagesByPropertyExecutor
}

func matchPropertyRouteUseCases(want propertyRouteUseCases) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(propertyRouteUseCasesFor, Equal(want))
}

func propertyRouteUseCasesFor(routes *Routes) propertyRouteUseCases {
	return propertyRouteUseCases{
		PropertyKeys:    routes.getPropertyKeys,
		PagesByProperty: routes.getPagesByProperty,
	}
}

type pagesByPropertyQueryObservation struct {
	Key   string
	Value string
}

func observedPagesByPropertyQuery(exec *recordingPagesByPropertyExecutor) pagesByPropertyQueryObservation {
	return pagesByPropertyQueryObservation{
		Key:   exec.seen.Key,
		Value: exec.seen.Value,
	}
}
