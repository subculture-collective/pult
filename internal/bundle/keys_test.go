package bundle

import (
	"testing"

	"github.com/jedisct1/go-minisign"
)

func TestTrustedKeysParse(t *testing.T) {
	if len(TrustedKeys) == 0 {
		t.Fatal("no trusted key")
	}
	seen := map[[8]byte]bool{}
	for _, k := range TrustedKeys {
		pk, err := minisign.NewPublicKey(k)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if pk.SignatureAlgorithm != [2]byte{'E', 'd'} {
			t.Errorf("%s: algorithm %q", k, pk.SignatureAlgorithm)
		}
		if seen[pk.KeyId] {
			t.Errorf("%s: duplicate key id", k)
		}
		seen[pk.KeyId] = true
		t.Logf("key id %X", pk.KeyId)
	}
}
