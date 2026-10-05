// Package ai provides habctl's LLM features (suggestions, reviews, chains,
// goal decomposition) on top of the suite-wide provider layer in
// missionctl-core/ai. This file only adapts it: the HABCTL_PROVIDER prefix,
// and Gemini via Google OAuth (GOOGLE_REFRESH_TOKEN) which only habctl offers.
package ai

import (
	"os"

	"github.com/aeon022/habctl/internal/auth"
	coreai "github.com/aeon022/missionctl-core/ai"
)

type (
	Provider     = coreai.Provider
	ProviderInfo = coreai.ProviderInfo
)

const (
	ProviderAnthropic = coreai.ProviderAnthropic
	ProviderOpenAI    = coreai.ProviderOpenAI
	ProviderGemini    = coreai.ProviderGemini
	ProviderOllama    = coreai.ProviderOllama
)

// Call dispatches to the active provider; ctx cancels in-flight requests.
var Call = coreai.Call

// Detect returns the active provider; HABCTL_PROVIDER overrides auto-detection.
func Detect() (ProviderInfo, error) { return coreai.Detect("HABCTL") }

func init() {
	coreai.GeminiAvailable = func() bool { return os.Getenv("GOOGLE_REFRESH_TOKEN") != "" }
	// A Google OAuth access token is accepted by the Gemini endpoint as a
	// bearer credential; "" falls back to the plain GEMINI_API_KEY.
	coreai.GeminiKey = func() string {
		rt, id, secret := os.Getenv("GOOGLE_REFRESH_TOKEN"), os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET")
		if rt == "" || id == "" || secret == "" {
			return ""
		}
		tok, err := auth.GetAccessToken(id, secret, rt)
		if err != nil {
			return ""
		}
		return tok
	}
}
