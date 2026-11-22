package network

import (
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestParsePlayitOutputLine(t *testing.T) {
	var st models.TunnelState
	ParsePlayitOutputLine("\x1b[32mhttps://playit.gg/claim/5c9953786b\x1b[0m", &st)
	if st.ClaimURL != "https://playit.gg/claim/5c9953786b" {
		t.Fatalf("unexpected ClaimURL: %q", st.ClaimURL)
	}
	if len(st.Addresses) != 0 || st.JavaAddress != "" {
		t.Fatalf("claim URL should not be treated as tunnel address: %+v", st)
	}

	ParsePlayitOutputLine("Connecting to api.playit.gg and https://playit.gg/account/agents", &st)
	if len(st.Addresses) != 0 {
		t.Fatalf("api.playit.gg/account URL should be ignored, got: %v", st.Addresses)
	}

	ParsePlayitOutputLine(`2026-11-22 INFO playitd::daemon: playit connected; tunnels loaded agent_id=112fcdb0-eefb-4abd-ba61-48ab72afd9e4 tunnel_count=1 pending_tunnel_count=0 disabled_tunnel_count=1 account_status="email_not_verified"`, &st)
	if st.DashboardURL != "https://playit.gg/account/agents/112fcdb0-eefb-4abd-ba61-48ab72afd9e4" {
		t.Fatalf("unexpected DashboardURL: %q", st.DashboardURL)
	}
	if st.AccountStatus != "email_not_verified" || st.DisabledTunnels != 1 {
		t.Fatalf("unexpected v2 account status / disabled count: %+v", st)
	}

	ParsePlayitOutputLine("Java tunnel active: sekai-smp.joinmc.link => 127.0.0.1:25565", &st)
	if st.JavaAddress != "sekai-smp.joinmc.link" {
		t.Fatalf("unexpected JavaAddress: %q", st.JavaAddress)
	}
	ParsePlayitOutputLine("Bedrock UDP tunnel: bedrock-hub.at.ply.gg:19132 => 127.0.0.1:19132", &st)
	if st.BedrockAddress != "bedrock-hub.at.ply.gg:19132" {
		t.Fatalf("unexpected BedrockAddress: %q", st.BedrockAddress)
	}
}

func TestExtractClaimSecretKey(t *testing.T) {
	if got := extractClaimSecretKey("abcdef0123456789\n"); got != "abcdef0123456789" {
		t.Fatalf("unexpected key: %q", got)
	}
	if got := extractClaimSecretKey("secret_key = \"xyz_secret_123\"\n"); got != "xyz_secret_123" {
		t.Fatalf("unexpected key from toml line: %q", got)
	}
}
