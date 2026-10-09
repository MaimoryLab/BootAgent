package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

const multimodalKey = "sk-multimodal-secret"

// multimodalGateway serves /models from a fixed listing and answers chat
// requests by model, recording every request so a test can prove detection
// sends nothing billed.
type multimodalGateway struct {
	mu       sync.Mutex
	listing  string
	listCode int
	chat     map[string]int
	requests []string
}

func (g *multimodalGateway) Do(request *http.Request) (*http.Response, error) {
	var body struct {
		Model string `json:"model"`
	}
	if request.Body != nil {
		data, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(data, &body)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, request.Method+" "+request.URL.Path+" "+body.Model)
	if request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/models") {
		code := g.listCode
		if code == 0 {
			code = http.StatusOK
		}
		return appProviderResponse(code, g.listing), nil
	}
	status, ok := g.chat[body.Model]
	if !ok {
		status = http.StatusOK
	}
	if status == http.StatusOK {
		return appProviderResponse(status, `{}`), nil
	}
	return appProviderResponse(status, `{"error":{"message":"model `+body.Model+` does not support image input, key `+multimodalKey+`"}}`), nil
}

func (g *multimodalGateway) billed() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var posts []string
	for _, request := range g.requests {
		if strings.HasPrefix(request, http.MethodPost) {
			posts = append(posts, request)
		}
	}
	return posts
}

func multimodalCore(t *testing.T, gateway *multimodalGateway) *UseCases {
	t.Helper()
	core := activationCore(t, t.TempDir(), provider.NewClient(gateway), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "gw", Name: "Gateway", BaseURL: "https://gw.example/v1", APIKey: multimodalKey,
	}, true, false); err != nil {
		t.Fatal(err)
	}
	return core
}

const gatewayListing = `{"data":[
	{"id":"qwen/qwen3-vl-8b","input_modalities":["text","image"],"output_modalities":["text"]},
	{"id":"moonshotai/kimi-k3"},
	{"id":"deepseek/deepseek-v4-pro","input_modalities":["text"],"output_modalities":["text"]}
]}`

func findModel(t *testing.T, detection MultimodalDetection, providerID, model string) modality.Model {
	t.Helper()
	for _, entry := range detection.Providers {
		if entry.ID != providerID {
			continue
		}
		for _, candidate := range entry.Models {
			if candidate.ID == model {
				return candidate
			}
		}
	}
	t.Fatalf("%s/%s not detected in %#v", providerID, model, detection)
	return modality.Model{}
}

// Opening the Skills page lists models, which is free. It must never probe.
func TestDetectMultimodalListsWithoutBilledRequests(t *testing.T) {
	gateway := &multimodalGateway{listing: gatewayListing}
	core := multimodalCore(t, gateway)
	detection, err := core.DetectMultimodal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if posts := gateway.billed(); len(posts) != 0 {
		t.Fatalf("detection sent billed requests: %v", posts)
	}
	// Only the Provider with a key is listed; built-ins without one are not.
	if len(detection.Providers) != 1 || detection.Providers[0].ID != "gw" || !detection.Providers[0].Listed {
		t.Fatalf("providers = %#v", detection.Providers)
	}
	declared := findModel(t, detection, "gw", "qwen/qwen3-vl-8b").Capabilities[0]
	if declared.Source != modality.SourceDeclared || !declared.Installable || declared.Probe != modality.ProbeMinimal {
		t.Fatalf("declared vision = %#v", declared)
	}
	guessed := findModel(t, detection, "gw", "moonshotai/kimi-k3").Capabilities[0]
	if guessed.Source != modality.SourceIDRule || guessed.Installable {
		t.Fatalf("ID-rule vision = %#v", guessed)
	}
	data, _ := json.Marshal(detection)
	if strings.Contains(string(data), multimodalKey) {
		t.Fatal("detection result carries the API key")
	}
}

func TestDetectMultimodalReportsAFailedListing(t *testing.T) {
	gateway := &multimodalGateway{listCode: http.StatusUnauthorized, listing: `{}`}
	core := multimodalCore(t, gateway)
	detection, err := core.DetectMultimodal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	entry := detection.Providers[0]
	if entry.Listed || entry.ErrorCode == "" || entry.Message == "" || entry.Models == nil {
		t.Fatalf("failed listing = %#v", entry)
	}
}

func TestProbeMultimodalCachesAPassAndOutranksTheIDRule(t *testing.T) {
	gateway := &multimodalGateway{listing: gatewayListing}
	core := multimodalCore(t, gateway)
	check, err := core.ProbeMultimodal(context.Background(), "gw", "moonshotai/kimi-k3", modality.Vision)
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != ModelCheckAvailable || len(gateway.billed()) != 1 {
		t.Fatalf("check = %#v, requests = %v", check, gateway.billed())
	}
	detection, err := core.DetectMultimodal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	vision := findModel(t, detection, "gw", "moonshotai/kimi-k3").Capabilities[0]
	if vision.Source != modality.SourceProbe || !vision.Installable || !vision.Supported {
		t.Fatalf("probed vision = %#v", vision)
	}
}

func TestProbeMultimodalCachesARefusalButNotAKeyProblem(t *testing.T) {
	gateway := &multimodalGateway{listing: gatewayListing, chat: map[string]int{
		"qwen/qwen3-vl-8b":   http.StatusBadRequest,
		"moonshotai/kimi-k3": http.StatusUnauthorized,
	}}
	core := multimodalCore(t, gateway)
	refused, err := core.ProbeMultimodal(context.Background(), "gw", "qwen/qwen3-vl-8b", modality.Vision)
	if err != nil {
		t.Fatal(err)
	}
	if refused.Status != ModelCheckUnavailable || refused.Reason != "refused" || refused.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("refused = %#v", refused)
	}
	if strings.Contains(refused.Detail, multimodalKey) || !strings.Contains(refused.Detail, "does not support image input") {
		t.Fatalf("detail = %q", refused.Detail)
	}
	key, err := core.ProbeMultimodal(context.Background(), "gw", "moonshotai/kimi-k3", modality.Vision)
	if err != nil {
		t.Fatal(err)
	}
	if key.Status != ModelCheckUnavailable || key.Reason != "key" {
		t.Fatalf("key = %#v", key)
	}
	detection, err := core.DetectMultimodal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	qwen := findModel(t, detection, "gw", "qwen/qwen3-vl-8b").Capabilities[0]
	if qwen.Source != modality.SourceProbe || qwen.Supported || qwen.Installable {
		t.Fatalf("refusal not cached: %#v", qwen)
	}
	// A rejected key says nothing about the model: the ID rule still answers.
	kimi := findModel(t, detection, "gw", "moonshotai/kimi-k3").Capabilities[0]
	if kimi.Source != modality.SourceIDRule {
		t.Fatalf("key failure was cached: %#v", kimi)
	}
	data, err := os.ReadFile(core.capabilities.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), multimodalKey) {
		t.Fatal("capability cache carries the API key")
	}
}

func TestProbeMultimodalRefusesWhatItCannotCheck(t *testing.T) {
	gateway := &multimodalGateway{listing: gatewayListing}
	core := multimodalCore(t, gateway)
	for _, capability := range []string{modality.VideoGeneration, modality.ImageGeneration, modality.TextToSpeech, "smell"} {
		if _, err := core.ProbeMultimodal(context.Background(), "gw", "m", capability); err == nil {
			t.Errorf("%s probe accepted", capability)
		}
	}
	if _, err := core.ProbeMultimodal(context.Background(), "gw", " ", modality.Vision); err == nil {
		t.Error("probe without a model accepted")
	}
	if posts := gateway.billed(); len(posts) != 0 {
		t.Fatalf("refused probes still sent requests: %v", posts)
	}
}

func TestProbeMultimodalWithoutAKeySendsNothing(t *testing.T) {
	gateway := &multimodalGateway{listing: gatewayListing}
	core := activationCore(t, t.TempDir(), provider.NewClient(gateway), "linux")
	check, err := core.ProbeMultimodal(context.Background(), "novita", "qwen/qwen3-vl-8b", modality.Vision)
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != ModelCheckUnverified || check.Reason != "no_key" || len(gateway.billed()) != 0 {
		t.Fatalf("check = %#v, requests = %v", check, gateway.billed())
	}
}
