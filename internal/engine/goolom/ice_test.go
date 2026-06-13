package goolom

import "testing"

func TestParseICEServerKeepsTURNWithCredentials(t *testing.T) {
	raw := map[string]any{
		"urls":       []any{"stun:stun.example:3478", "turn:turn.example:3478", "turns:turn.example:5349"},
		"username":   "u",
		"credential": "c",
	}
	ice, ok := parseICEServer(raw)
	if !ok {
		t.Fatal("parseICEServer dropped a server that has TURN urls")
	}
	if len(ice.URLs) != 3 {
		t.Fatalf("parseICEServer URLs = %v, want stun+turn+turns kept", ice.URLs)
	}
	if ice.Username != "u" || ice.Credential != "c" {
		t.Fatalf("parseICEServer creds = %q/%v, want u/c", ice.Username, ice.Credential)
	}
}

func TestParseICEURLsRejectsJunk(t *testing.T) {
	urls := parseICEURLs(map[string]any{"urls": []any{"http://nope", "", "turn:ok:3478"}})
	if len(urls) != 1 || urls[0] != "turn:ok:3478" {
		t.Fatalf("parseICEURLs = %v, want only [turn:ok:3478]", urls)
	}
}
