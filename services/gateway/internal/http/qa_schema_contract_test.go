package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type openAPIOperation struct {
	Method      string
	Path        string
	Owner       string
	OperationID string
	Operation   map[string]any
	PathItem    map[string]any
}

func TestQAActiveOpenAPIContractsHaveSchemasAndAuth(t *testing.T) {
	document := readOpenAPIDocument(t, gatewayOpenAPIPath(t))
	operations := ownerOpenAPIOperations(t, document, "qa")
	if got, want := len(operations), 29; got != want {
		t.Fatalf("qa active operations = %d, want %d", got, want)
	}

	routes := routesByMethodPath(activeProxyRoutes)
	seen := map[string]bool{}
	for _, operation := range operations {
		key := operation.Method + " " + operation.Path
		seen[key] = true
		route, ok := routes[key]
		if !ok {
			t.Fatalf("%s is missing from activeProxyRoutes", key)
		}
		if route.Owner != "qa" || route.OperationID != operation.OperationID {
			t.Fatalf("%s route owner/operationId = %q/%q, want qa/%q", key, route.Owner, route.OperationID, operation.OperationID)
		}
		if !operationRequiresBearerAuth(document, operation.Operation) {
			t.Fatalf("%s operationId=%s must require bearerAuth", key, operation.OperationID)
		}

		parameters := operationParameters(t, document, operation)
		assertPathParametersHaveSchemas(t, operation, parameters)
		if qaOperationRequiresPagination(operation.OperationID) {
			assertQueryParameterSchema(t, operation, parameters, "page", "integer")
			assertQueryParameterSchema(t, operation, parameters, "pageSize", "integer")
		}

		if operation.Method == http.MethodPost || operation.Method == http.MethodPatch {
			if operation.OperationID == "uploadQASessionAttachment" {
				assertMultipartRequestSchema(t, document, operation)
			} else {
				assertJSONRequestSchema(t, document, operation)
			}
		}
		assertQASuccessResponseSchemas(t, document, operation)
		assertQAErrorResponseSchemas(t, document, operation)
	}

	for key, route := range routes {
		if route.Owner == "qa" && !seen[key] {
			t.Fatalf("qa route %s operationId=%s missing from Gateway OpenAPI", key, route.OperationID)
		}
	}
}

func TestQASseEventSchemaCoversSafePublicEvents(t *testing.T) {
	document := readOpenAPIDocument(t, gatewayOpenAPIPath(t))
	schema := resolveOpenAPIRef(t, document, "#/components/schemas/QASseEventType")
	enumValues := stringsFromAnySlice(schema["enum"])

	required := []string{
		"message.created",
		"agent.iteration.started",
		"reasoning.step",
		"reasoning.delta",
		"tool.started",
		"tool.completed",
		"tool.failed",
		"answer.delta",
		"citation.delta",
		"answer.completed",
		"error",
	}
	for _, event := range required {
		if !containsString(enumValues, event) {
			t.Fatalf("QASseEventType missing %q; enum=%v", event, enumValues)
		}
	}
	if !containsString(enumValues, "heartbeat") {
		t.Fatalf("QASseEventType should allow transport heartbeat events; enum=%v", enumValues)
	}

	forbidden := []string{"prompt", "chain", "thought", "raw", "objectKey", "internalUrl", "provider"}
	for _, value := range enumValues {
		for _, marker := range forbidden {
			if strings.Contains(strings.ToLower(value), strings.ToLower(marker)) {
				t.Fatalf("QASseEventType exposes unsafe event name %q", value)
			}
		}
	}
}

func TestQAInternalOpenAPIRefsCoverGatewayActivePaths(t *testing.T) {
	gatewayDocument := readOpenAPIDocument(t, gatewayOpenAPIPath(t))
	operations := ownerOpenAPIOperations(t, gatewayDocument, "qa")

	for _, relativePath := range []string{
		"docs/services/qa/api/internal.openapi.yaml",
		"services/qa/api/openapi.yaml",
	} {
		t.Run(relativePath, func(t *testing.T) {
			document := readOpenAPIDocument(t, projectPath(t, relativePath))
			paths := requiredMap(t, document, "paths")
			for _, operation := range operations {
				internalPath := "/internal/v1" + strings.TrimPrefix(operation.Path, "/api/v1")
				pathItem := requiredNestedMap(t, paths, internalPath)
				ref, ok := pathItem["$ref"].(string)
				if !ok || strings.TrimSpace(ref) == "" {
					t.Fatalf("%s missing $ref for %s", internalPath, operation.OperationID)
				}
				wantPointer := "#/paths/" + escapeJSONPointer(operation.Path)
				if !strings.HasSuffix(ref, wantPointer) || !strings.Contains(ref, "gateway/api/public.openapi.yaml") {
					t.Fatalf("%s $ref = %q, want Gateway public OpenAPI pointer ending %q", internalPath, ref, wantPointer)
				}
			}
		})
	}
}

func TestQAProxyRoutesDefaultToInternalNamespace(t *testing.T) {
	for _, route := range activeProxyRoutes {
		if route.Owner != "qa" {
			continue
		}
		if route.DownstreamPattern != "" {
			t.Fatalf("qa route %s %s uses custom downstream pattern %q; expected default internal namespace mapping", route.Method, route.Pattern, route.DownstreamPattern)
		}
		publicPath := samplePath(route.Pattern)
		req := httptest.NewRequest(route.Method, publicPath, nil)
		got := route.downstreamPath(req)
		want := "/internal/v1" + strings.TrimPrefix(publicPath, "/api/v1")
		if got != want {
			t.Fatalf("qa route %s %s downstream path = %q, want %q", route.Method, route.Pattern, got, want)
		}
	}
}

func TestQAProxyPreservesPathParametersAndQuery(t *testing.T) {
	route := routeSpec{Method: http.MethodGet, Pattern: "/api/v1/qa-sessions/{sessionId}/events", Owner: "qa", OperationID: "listQAStreamEvents"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/qa-sessions/sess_1/events?responseRunId=run_1&afterEventSeq=2", nil)
	baseURL, err := url.Parse("http://qa.internal/base")
	if err != nil {
		t.Fatal(err)
	}

	targetURL := *baseURL
	targetURL.Path = joinProxyPath(baseURL.Path, route.downstreamPath(req))
	targetURL.RawQuery = req.URL.RawQuery

	if targetURL.Path != "/base/internal/v1/qa-sessions/sess_1/events" {
		t.Fatalf("downstream path = %q", targetURL.Path)
	}
	if targetURL.RawQuery != "responseRunId=run_1&afterEventSeq=2" {
		t.Fatalf("downstream query = %q", targetURL.RawQuery)
	}
}

func readOpenAPIDocument(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse OpenAPI YAML %s: %v", path, err)
	}
	return document
}

func ownerOpenAPIOperations(t *testing.T, document map[string]any, owner string) []openAPIOperation {
	t.Helper()
	paths := requiredMap(t, document, "paths")
	var operations []openAPIOperation
	for path, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			continue
		}
		for method, rawOperation := range pathItem {
			if !isHTTPMethod(method) {
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Fatalf("%s %s operation is not an object", strings.ToUpper(method), path)
			}
			if operation["x-owner-service"] != owner {
				continue
			}
			operationID, ok := operation["operationId"].(string)
			if !ok || operationID == "" {
				t.Fatalf("%s %s missing operationId", strings.ToUpper(method), path)
			}
			operations = append(operations, openAPIOperation{
				Method:      strings.ToUpper(method),
				Path:        path,
				Owner:       owner,
				OperationID: operationID,
				Operation:   operation,
				PathItem:    pathItem,
			})
		}
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Path == operations[j].Path {
			return operations[i].Method < operations[j].Method
		}
		return operations[i].Path < operations[j].Path
	})
	return operations
}

func routesByMethodPath(routes []routeSpec) map[string]routeSpec {
	result := map[string]routeSpec{}
	for _, route := range routes {
		result[route.Method+" "+route.Pattern] = route
	}
	return result
}

func operationRequiresBearerAuth(document map[string]any, operation map[string]any) bool {
	security, ok := operation["security"]
	if !ok {
		security = document["security"]
	}
	for _, value := range anySlice(security) {
		requirement, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := requirement["bearerAuth"]; ok {
			return true
		}
	}
	return false
}

func operationParameters(t *testing.T, document map[string]any, operation openAPIOperation) []map[string]any {
	t.Helper()
	var parameters []map[string]any
	for _, value := range append(anySlice(operation.PathItem["parameters"]), anySlice(operation.Operation["parameters"])...) {
		parameter := resolveOpenAPIMapValue(t, document, value)
		if parameter == nil {
			t.Fatalf("%s %s contains non-object parameter %#v", operation.Method, operation.Path, value)
		}
		parameters = append(parameters, parameter)
	}
	return parameters
}

func assertPathParametersHaveSchemas(t *testing.T, operation openAPIOperation, parameters []map[string]any) {
	t.Helper()
	for _, name := range pathTemplateParameters(operation.Path) {
		parameter := findParameter(parameters, "path", name)
		if parameter == nil {
			t.Fatalf("%s %s missing path parameter %s", operation.Method, operation.Path, name)
		}
		if required, _ := parameter["required"].(bool); !required {
			t.Fatalf("%s %s path parameter %s must be required", operation.Method, operation.Path, name)
		}
		schema := mapValue(parameter["schema"])
		if schema == nil {
			t.Fatalf("%s %s path parameter %s missing schema", operation.Method, operation.Path, name)
		}
		if got := schema["type"]; got != "string" {
			t.Fatalf("%s %s path parameter %s type = %v, want string", operation.Method, operation.Path, name, got)
		}
	}
}

func assertQueryParameterSchema(t *testing.T, operation openAPIOperation, parameters []map[string]any, name string, wantType string) {
	t.Helper()
	parameter := findParameter(parameters, "query", name)
	if parameter == nil {
		t.Fatalf("%s %s missing query parameter %s", operation.Method, operation.Path, name)
	}
	schema := mapValue(parameter["schema"])
	if schema == nil {
		t.Fatalf("%s %s query parameter %s missing schema", operation.Method, operation.Path, name)
	}
	if got := schema["type"]; got != wantType {
		t.Fatalf("%s %s query parameter %s type = %v, want %s", operation.Method, operation.Path, name, got, wantType)
	}
}

func assertJSONRequestSchema(t *testing.T, document map[string]any, operation openAPIOperation) {
	t.Helper()
	requestBody := resolveOpenAPIMapValue(t, document, operation.Operation["requestBody"])
	if requestBody == nil {
		t.Fatalf("%s %s missing JSON request body schema", operation.Method, operation.Path)
	}
	content := requiredNestedMap(t, requestBody, "content")
	mediaType := requiredNestedMap(t, content, "application/json")
	if mapValue(mediaType["schema"]) == nil {
		t.Fatalf("%s %s application/json request body missing schema", operation.Method, operation.Path)
	}
}

func assertMultipartRequestSchema(t *testing.T, document map[string]any, operation openAPIOperation) {
	t.Helper()
	requestBody := resolveOpenAPIMapValue(t, document, operation.Operation["requestBody"])
	if requestBody == nil {
		t.Fatalf("%s %s missing multipart request body schema", operation.Method, operation.Path)
	}
	content := requiredNestedMap(t, requestBody, "content")
	mediaType := requiredNestedMap(t, content, "multipart/form-data")
	if mapValue(mediaType["schema"]) == nil {
		t.Fatalf("%s %s multipart/form-data request body missing schema", operation.Method, operation.Path)
	}
}

func assertQASuccessResponseSchemas(t *testing.T, document map[string]any, operation openAPIOperation) {
	t.Helper()
	successes := successResponses(t, operation)
	if len(successes) == 0 {
		t.Fatalf("%s %s has no success response", operation.Method, operation.Path)
	}
	for status, response := range successes {
		resolvedResponse := resolveOpenAPIMapValue(t, document, response)
		if status == "204" {
			if content, ok := resolvedResponse["content"]; ok && len(mapValue(content)) > 0 {
				t.Fatalf("%s %s 204 response must not define content", operation.Method, operation.Path)
			}
			continue
		}
		content := requiredNestedMap(t, resolvedResponse, "content")
		jsonMediaType := requiredNestedMap(t, content, "application/json")
		schema := resolveSchemaValue(t, document, jsonMediaType["schema"])
		assertSuccessEnvelopeSchema(t, document, operation, status, schema, qaOperationRequiresPagination(operation.OperationID))

		if operation.OperationID == "createQAMessage" {
			sseMediaType := requiredNestedMap(t, content, "text/event-stream")
			sseSchema := mapValue(sseMediaType["schema"])
			if sseSchema == nil || sseSchema["type"] != "string" {
				t.Fatalf("%s %s text/event-stream schema = %#v, want string", operation.Method, operation.Path, sseMediaType["schema"])
			}
		} else if _, ok := content["text/event-stream"]; ok {
			t.Fatalf("%s %s unexpectedly declares text/event-stream", operation.Method, operation.Path)
		}
	}
}

func assertSuccessEnvelopeSchema(t *testing.T, document map[string]any, operation openAPIOperation, status string, schema map[string]any, requirePagedEnvelope bool) {
	t.Helper()
	required := stringsFromAnySlice(schema["required"])
	for _, field := range []string{"data", "requestId"} {
		if !containsString(required, field) {
			t.Fatalf("%s %s %s schema missing required %q; required=%v", operation.Method, operation.Path, status, field, required)
		}
	}
	properties := requiredNestedMap(t, schema, "properties")
	if mapValue(properties["data"]) == nil {
		t.Fatalf("%s %s %s schema missing data property", operation.Method, operation.Path, status)
	}
	if mapValue(properties["requestId"]) == nil {
		t.Fatalf("%s %s %s schema missing requestId property", operation.Method, operation.Path, status)
	}
	if requirePagedEnvelope && !containsString(required, "page") {
		t.Fatalf("%s %s %s schema must require page envelope; required=%v", operation.Method, operation.Path, status, required)
	}
	if containsString(required, "page") {
		pageSchema := resolveSchemaValue(t, document, properties["page"])
		pageRequired := stringsFromAnySlice(pageSchema["required"])
		for _, field := range []string{"page", "pageSize", "total"} {
			if !containsString(pageRequired, field) {
				t.Fatalf("%s %s %s page schema missing required %q; required=%v", operation.Method, operation.Path, status, field, pageRequired)
			}
		}
	}
}

func assertQAErrorResponseSchemas(t *testing.T, document map[string]any, operation openAPIOperation) {
	t.Helper()
	seenErrorResponse := false
	for status, rawResponse := range responseMap(t, operation) {
		if !strings.HasPrefix(status, "4") && !strings.HasPrefix(status, "5") {
			continue
		}
		seenErrorResponse = true
		response := resolveOpenAPIMapValue(t, document, rawResponse)
		content := requiredNestedMap(t, response, "content")
		mediaType := requiredNestedMap(t, content, "application/json")
		schema := resolveSchemaValue(t, document, mediaType["schema"])
		required := stringsFromAnySlice(schema["required"])
		if !containsString(required, "error") {
			t.Fatalf("%s %s %s error schema missing required error envelope; required=%v", operation.Method, operation.Path, status, required)
		}
		properties := requiredNestedMap(t, schema, "properties")
		errorSchema := resolveSchemaValue(t, document, properties["error"])
		errorRequired := stringsFromAnySlice(errorSchema["required"])
		for _, field := range []string{"code", "message", "requestId"} {
			if !containsString(errorRequired, field) {
				t.Fatalf("%s %s %s ErrorDetail missing required %q; required=%v", operation.Method, operation.Path, status, field, errorRequired)
			}
		}
	}
	if !seenErrorResponse {
		t.Fatalf("%s %s must declare at least one 4xx/5xx ErrorResponse", operation.Method, operation.Path)
	}
}

func successResponses(t *testing.T, operation openAPIOperation) map[string]any {
	t.Helper()
	result := map[string]any{}
	for status, response := range responseMap(t, operation) {
		if strings.HasPrefix(status, "2") {
			result[status] = response
		}
	}
	return result
}

func qaOperationRequiresPagination(operationID string) bool {
	switch operationID {
	case "listQASessions", "listQAMessages":
		return true
	default:
		return false
	}
}

func responseMap(t *testing.T, operation openAPIOperation) map[string]any {
	t.Helper()
	return requiredNestedMap(t, operation.Operation, "responses")
}

func resolveSchemaValue(t *testing.T, document map[string]any, value any) map[string]any {
	t.Helper()
	schema := resolveOpenAPIMapValue(t, document, value)
	if schema == nil {
		t.Fatalf("schema is not an object: %#v", value)
	}
	return schema
}

func resolveOpenAPIMapValue(t *testing.T, document map[string]any, value any) map[string]any {
	t.Helper()
	current := mapValue(value)
	if current == nil {
		return nil
	}
	ref, _ := current["$ref"].(string)
	if ref == "" {
		return current
	}
	return resolveOpenAPIRef(t, document, ref)
}

func resolveOpenAPIRef(t *testing.T, document map[string]any, ref string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(ref, "#/") {
		t.Fatalf("unsupported external OpenAPI ref %q", ref)
	}
	current := any(document)
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		key := unescapeJSONPointer(segment)
		m, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("ref %q reached non-object before %q", ref, key)
		}
		current, ok = m[key]
		if !ok {
			t.Fatalf("ref %q missing segment %q", ref, key)
		}
	}
	result := mapValue(current)
	if result == nil {
		t.Fatalf("ref %q resolved to non-object %#v", ref, current)
	}
	return result
}

func requiredMap(t *testing.T, document map[string]any, key string) map[string]any {
	t.Helper()
	return requiredNestedMap(t, document, key)
}

func requiredNestedMap(t *testing.T, source map[string]any, key string) map[string]any {
	t.Helper()
	result := mapValue(source[key])
	if result == nil {
		t.Fatalf("missing object key %q in %#v", key, source)
	}
	return result
}

func mapValue(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return nil
}

func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func stringsFromAnySlice(value any) []string {
	var result []string
	for _, item := range anySlice(value) {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func findParameter(parameters []map[string]any, location string, name string) map[string]any {
	for _, parameter := range parameters {
		if parameter["in"] == location && parameter["name"] == name {
			return parameter
		}
	}
	return nil
}

func pathTemplateParameters(path string) []string {
	var result []string
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			result = append(result, strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}"))
		}
	}
	return result
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func projectPath(t *testing.T, relativePath string) string {
	t.Helper()
	dir := filepath.Dir(gatewayOpenAPIPath(t))
	for i := 0; i < 12; i++ {
		candidate := filepath.Join(dir, relativePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("%s not found from gateway OpenAPI path", relativePath)
	return ""
}

func escapeJSONPointer(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

func unescapeJSONPointer(value string) string {
	value = strings.ReplaceAll(value, "~1", "/")
	return strings.ReplaceAll(value, "~0", "~")
}
