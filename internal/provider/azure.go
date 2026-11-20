package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/istoo/owl/internal/catalog"
	"github.com/istoo/owl/internal/lazyre"
)

// Azure OpenAI is reached at the user's own resource, never at one host
// owl knows: https://<resource>.openai.azure.com (or the
// cognitiveservices.azure.com and services.ai.azure.com hosts newer
// resources are given). owl asks it on the v1 API under /openai/v1 —
// Azure's generally available one since August 2025 — where chat
// completions, Responses and the model list sit at the paths OpenAI's do,
// the model in the body is the deployment's name, and no api-version is
// wanted. The classic /openai/deployments/<deployment>/…?api-version=
// paths are not used: every endpoint, however it was pasted, is asked at
// /openai/v1. The key goes in api-key, never as a Bearer: a Bearer on
// Azure is an Entra ID token.

// AzurePreset is the preset's id.
const AzurePreset = "azure"

// azureHosts end the hosts an Azure OpenAI resource is served at.
var azureHosts = []string{".openai.azure.com", ".cognitiveservices.azure.com", ".services.ai.azure.com"}

// AzureHost reports whether host is an Azure OpenAI resource's.
func AzureHost(host string) bool {
	host = strings.ToLower(host)
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	for _, s := range azureHosts {
		if strings.HasSuffix(host, s) && len(host) > len(s) {
			return true
		}
	}
	return false
}

// IsAzure reports whether the provider is Azure OpenAI: made from its
// preset, or at a resource's host.
func (p Provider) IsAzure() bool {
	if p.Preset == AzurePreset {
		return true
	}
	for _, u := range []string{p.Chat, p.Responses} {
		if u != "" && AzureHost(HostOf(u)) {
			return true
		}
	}
	return false
}

// AzureBase is the v1 API base of the resource an endpoint names, as it
// was pasted from the portal or another app — the bare resource
// (https://r.openai.azure.com), …/openai, …/openai/v1, a classic
// deployment's URL with its api-version, or only the resource's name —
// and ok false when it names no Azure resource.
func AzureBase(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if !strings.ContainsAny(host, ".:") {
		// the resource's name alone, as the portal lists it
		host += ".openai.azure.com"
	}
	if !AzureHost(host) {
		return "", false
	}
	return "https://" + host + "/openai/v1", true
}

// azureEndpoints puts an Azure provider's chat completions and Responses
// on its resource's v1 API: both are served there, whichever of them the
// user or the app it came from gave. A provider at a host that is no
// resource's (an API Management gateway in front of one, or a test's
// server) keeps its URLs.
func (p *Provider) azureEndpoints() {
	given := p.Chat
	if given == "" {
		given = p.Responses
	}
	if given == "" {
		return
	}
	if h := HostOf(given); strings.HasPrefix(h, "localhost") || p.Preset != AzurePreset && !AzureHost(h) {
		return
	}
	if base, ok := AzureBase(given); ok {
		p.Chat, p.Responses = base, base
	}
}

// asAzure makes a provider brought from another app the Azure preset's,
// its endpoints on the resource's v1 API.
func asAzure(p Provider) Provider {
	pr := Preset(AzurePreset)
	p.Preset, p.Icon, p.Catalog, p.Website, p.KeysURL = pr.ID, pr.Icon, pr.Catalog, pr.Website, pr.KeysURL
	if p.ID == "" || p.ID == "default" {
		p.ID = pr.ID
	}
	if n := strings.ToLower(p.Name); n == "" || n == "default" || n == "azure" {
		p.Name = pr.Name
	}
	p.azureEndpoints()
	return p
}

// azureDeploymentsVersion is the api-version the data plane's list of
// deployments is asked with: later versions dropped the list, this one
// still answers a key.
const azureDeploymentsVersion = "2022-12-01"

// azureModels lists the resource's deployments — their names are the model
// ids its v1 API takes — and, where the data plane won't list them, the
// models the v1 API lists, which are the deployments' ids only when each is
// named after its model. Asked with the key in api-key alone.
func (p Provider) azureModels(ctx context.Context) ([]catalog.Model, string, error) {
	base := strings.TrimRight(p.Chat, "/")
	if base == "" {
		base = strings.TrimRight(p.Responses, "/")
	}
	root := strings.TrimSuffix(base, "/v1")
	headers := map[string]string{}
	for k, v := range AuthHeaders(p, Chat) {
		headers[k] = v
	}
	for k, v := range p.SentHeaders(ctx) {
		// the user's api-key, in any case, is the one sent
		for had := range headers {
			if strings.EqualFold(had, k) {
				delete(headers, had)
			}
		}
		headers[k] = v
	}
	urls := []string{
		root + "/deployments?api-version=" + azureDeploymentsVersion,
		base + "/models",
	}
	var errs []string
	for i, u := range urls {
		ms, err := catalog.FetchURL(ctx, u, "", false, headers)
		if err == nil {
			if i == 0 {
				// The data plane's list can lag behind a deployment made
				// since it last refreshed: a model's own empty chat says
				// whether the resource serves it.
				ms = append(ms, azureUnlisted(ctx, base, ms, headers)...)
			}
			return ms, base, nil
		}
		errs = append(errs, err.Error())
		if ctx.Err() != nil {
			break
		}
	}
	return nil, "", errorf("%s — type your deployments' names in as model ids", strings.Join(errs, "; "))
}

// azureProbeMost, azureProbeAt and azureProbeFor bound the check
// azureUnlisted makes: how many deployments it may ask about, how many of
// those requests may be in flight, and how long the whole check may take
// on top of the list that already answered. A refresh is a user waiting
// on it, so a slow or failing check never holds the list back.
const (
	azureProbeMost = 32
	azureProbeAt   = 4
	azureProbeFor  = 10 * time.Second
)

// azureDatedID matches a catalog id that carries its release date, such as
// gpt-6.2-sol-2026-11-17. A deployment is named for the undated model, so
// the dated aliases are not asked about.
var azureDatedID = lazyre.New(`-\d{4}-\d{2}-\d{2}$`)

// azureCatalogRow is a row of the resource's v1 model catalog. The catalog
// names every model the resource can deploy, which is not what it has
// deployed; created_at orders them, and inference is when the model stops
// being callable.
type azureCatalogRow struct {
	ID           string `json:"id"`
	CreatedAt    int64  `json:"created_at"`
	Created      int64  `json:"created"` // older catalog versions' name
	Capabilities struct {
		ChatCompletion bool `json:"chat_completion"`
	} `json:"capabilities"`
	Deprecation struct {
		Inference int64 `json:"inference"`
	} `json:"deprecation"`
}

func (r azureCatalogRow) createdAt() int64 {
	if r.CreatedAt != 0 {
		return r.CreatedAt
	}
	return r.Created
}

// azureUnlisted looks for deployments the data plane's list left out. New
// deployments are the ones missing from it, so the catalog's models made
// no earlier than the newest deployed model are each given an empty chat
// request: the v1 API answers 400 for a deployment it serves (the empty
// messages are what it objects to) and 404, or a 400 naming deploymentless
// inference, for one it does not. Every other answer — a 5xx, a 429, a
// read that fails — means "unknown" and is left out of the list.
//
// debt: only a model the catalog says chat-completes, has no dated id, is
// not listed, and is no older than the newest deployed model is asked
// about, at most azureProbeMost of them, newest first. A deployment of an
// older model under a new name is still missed, and so is any deployment
// beyond the cap. Upgrade by asking a data plane version that lists
// deployments again, or by remembering which ids a resource's earlier
// lists lacked and asking about those first.
func azureUnlisted(ctx context.Context, base string, listed []catalog.Model, headers map[string]string) []catalog.Model {
	ctx, cancel := context.WithTimeout(ctx, azureProbeFor)
	defer cancel()
	body, err := azureGet(ctx, base+"/models", headers)
	if err != nil {
		return nil // what the catalog says is unknown; the list stands
	}
	var v struct {
		Data []azureCatalogRow `json:"data"`
	}
	if json.Unmarshal(body, &v) != nil {
		return nil
	}
	here := make(map[string]bool, len(listed))
	for _, m := range listed {
		here[m.ID] = true
	}
	var newest int64
	for _, r := range v.Data {
		if here[r.ID] && r.createdAt() > newest {
			newest = r.createdAt()
		}
	}
	if newest == 0 {
		// no deployed model is in the catalog, so there is no "newer than
		// what is deployed" to judge a missing one by
		return nil
	}
	now := time.Now().Unix()
	var cands []azureCatalogRow
	for _, r := range v.Data {
		if r.ID == "" || here[r.ID] || !r.Capabilities.ChatCompletion {
			continue
		}
		if r.createdAt() < newest || azureDatedID.MatchString(r.ID) {
			continue
		}
		if d := r.Deprecation.Inference; d != 0 && d <= now {
			continue
		}
		cands = append(cands, r)
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].createdAt() > cands[j].createdAt() })
	if len(cands) > azureProbeMost {
		cands = cands[:azureProbeMost]
	}
	got := make([]bool, len(cands))
	sem := make(chan struct{}, azureProbeAt)
	var wg sync.WaitGroup
	for i := range cands {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			got[i] = azureDeployed(ctx, base, cands[i].ID, headers)
		}(i)
	}
	wg.Wait()
	out := make([]catalog.Model, 0, len(cands))
	for i, r := range cands {
		if got[i] {
			out = append(out, catalog.Model{ID: r.ID, Name: r.ID})
		}
	}
	return out
}

// azureDeployed asks the resource's v1 API to chat on id with no messages,
// which is the cheapest request that tells a deployment it serves from a
// model it only has in its catalog. It is only ever called for a model the
// catalog lists and the deployments' list does not.
func azureDeployed(ctx context.Context, base, id string, headers map[string]string) bool {
	b, err := json.Marshal(map[string]any{"model": id, "messages": []any{}})
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "owl")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	switch res.StatusCode {
	case http.StatusBadRequest:
		// The deployment answered, and objected to the empty chat — unless
		// it says the model needs a deployment of its own.
		return !bytes.Contains(bytes.ToLower(body), []byte("does not support deploymentless inference"))
	case http.StatusNotFound:
		return false
	}
	return false
}

// azureGet reads the JSON body at u with the provider's headers.
func azureGet(ctx context.Context, u string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "owl")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 8<<20))
}
