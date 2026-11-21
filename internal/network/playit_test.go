package network

import (
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestParsePlayitOutputLine(t *testing.T) {
	var st models.TunnelState
	ParsePlayitOutputLine("https://playit.gg/claim/5c9953786b", &st)
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

	ParsePlayitOutputLine("Java tunnel active: sekai-smp.joinmc.link => 127.0.0.1:25565", &st)
	if st.JavaAddress != "sekai-smp.joinmc.link" {
		t.Fatalf("unexpected JavaAddress: %q", st.JavaAddress)
	}
	ParsePlayitOutputLine("Bedrock UDP tunnel: bedrock-hub.at.ply.gg:19132 => 127.0.0.1:19132", &st)
	if st.BedrockAddress != "bedrock-hub.at.ply.gg:19132" {
		t.Fatalf("unexpected BedrockAddress: %q", st.BedrockAddress)
	}
}
