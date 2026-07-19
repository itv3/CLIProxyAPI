package helps

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/officialclient"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func ResolveOfficialClientCompatibility(auth *cliproxyauth.Auth, headers http.Header, connectivity bool) (officialclient.Decision, error) {
	if auth == nil {
		return officialclient.Decision{}, fmt.Errorf("%w: auth is nil", officialclient.ErrInvalidAttributes)
	}
	compatibility, err := officialclient.CompatibilityFromAttributes(auth.Attributes)
	if err != nil {
		return officialclient.Decision{}, err
	}
	return officialclient.Decide(officialclient.DecisionInput{
		Provider:       auth.Provider,
		APIKeyAccount:  strings.EqualFold(strings.TrimSpace(auth.Attributes["auth_kind"]), "apikey"),
		Compatibility:  compatibility,
		OfficialClient: officialclient.IsOfficialClient(auth.Provider, headers),
		Connectivity:   connectivity,
	})
}

func FinalizeOfficialClientHeaders(headers http.Header, provider string, desired map[string]string) {
	officialclient.FinalizeProtectedHeaders(headers, provider, desired)
}
