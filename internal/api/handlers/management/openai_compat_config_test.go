package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func performOpenAICompatRequest(method string, path string, body []byte, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	handler(c)
	return rec
}

func TestOpenAICompatManagementPutGetPatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	h := &Handler{cfg: &config.Config{}, configFilePath: configPath}

	putBody := []byte(`[
		{
			"name":" kimi ",
			"disabled": true,
			"prefix":" team ",
			"base-url":" https://kimi.example.com/v1 ",
			"api-key-entries":[
				{"api-key":" key-a ","disabled":true,"proxy-url":" http://127.0.0.1:7890 "},
				{"api-key":" key-b ","proxy-id":" hk "}
			],
			"models":[
				{"name":" kimi-k2.5 ","alias":" kimi-k2.6 "},
				{"name":" kimi-for-coding ","alias":" kimi-k2.6 "}
			]
		}
	]`)
	putRec := performOpenAICompatRequest(http.MethodPut, "/v0/management/openai-compatibility", putBody, h.PutOpenAICompat)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", putRec.Code, putRec.Body.String())
	}
	if len(h.cfg.OpenAICompatibility) != 1 {
		t.Fatalf("compat len = %d", len(h.cfg.OpenAICompatibility))
	}
	if !h.cfg.OpenAICompatibility[0].Disabled {
		t.Fatal("provider disabled flag not stored")
	}
	if len(h.cfg.OpenAICompatibility[0].APIKeyEntries) != 2 || !h.cfg.OpenAICompatibility[0].APIKeyEntries[0].Disabled {
		t.Fatalf("api-key-entries disabled flags = %+v", h.cfg.OpenAICompatibility[0].APIKeyEntries)
	}

	getRec := performOpenAICompatRequest(http.MethodGet, "/v0/management/openai-compatibility", nil, h.GetOpenAICompat)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	var getBody struct {
		Items []config.OpenAICompatibility `json:"openai-compatibility"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode GET body: %v", err)
	}
	if len(getBody.Items) != 1 || !getBody.Items[0].Disabled || !getBody.Items[0].APIKeyEntries[0].Disabled {
		t.Fatalf("GET body = %+v", getBody.Items)
	}

	patchBody := []byte(`{
		"name":"kimi",
		"value":{
			"disabled": false,
			"api-key-entries":[
				{"api-key":"key-a","disabled":false},
				{"api-key":"key-b","disabled":true,"proxy-id":"hk"}
			]
		}
	}`)
	patchRec := performOpenAICompatRequest(http.MethodPatch, "/v0/management/openai-compatibility", patchBody, h.PatchOpenAICompat)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d body=%s", patchRec.Code, patchRec.Body.String())
	}
	if h.cfg.OpenAICompatibility[0].Disabled {
		t.Fatal("provider disabled flag not cleared")
	}
	if h.cfg.OpenAICompatibility[0].APIKeyEntries[0].Disabled {
		t.Fatal("first key disabled flag not cleared")
	}
	if !h.cfg.OpenAICompatibility[0].APIKeyEntries[1].Disabled {
		t.Fatal("second key disabled flag not persisted")
	}
}
