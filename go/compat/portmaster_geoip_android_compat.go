package geoip

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/safing/portmaster/updates"
)

// verifyAndroidGeoIPHash checks the unpacked MMDB against the SHA-256 published
// by Safing's current v3 Intel index. Old Portmaster releases disabled Intel
// signatures and no longer receive these GeoIP entries through their legacy
// index, so this restores an integrity check at the point of use.
func verifyAndroidGeoIPHash(resource, filename string) error {
	expected := updates.AndroidGeoIPExpectedSHA256(resource)
	if expected == "" {
		return fmt.Errorf("missing expected SHA-256 for %s", resource)
	}

	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open GeoIP file for verification: %w", err)
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(file, 256<<20)); err != nil {
		return fmt.Errorf("hash GeoIP file: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("GeoIP SHA-256 mismatch for %s", resource)
	}
	return nil
}
