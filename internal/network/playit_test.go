package network

import (
	"testing"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestParsePlayitOutputLine(t *testing.T) {
	var st models.TunnelState
	ParsePlayitOutputLine("Visit link to claim agent: https://playit.gg/claim/ab12cd34ef", &st)
	if st.ClaimURL != "https://playit.gg/claim/ab12cd34ef" {
		t.Fatalf("unexpected ClaimURL: %q", st.ClaimURL)
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
