package updates

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/updater"
)

const androidIntelV3URL = "https://updates.safing.io/intel.v3.json"

type androidIntelV3Artifact struct {
	Filename string   `json:"Filename"`
	SHA256   string   `json:"SHA256"`
	URLs     []string `json:"URLs"`
	Unpack   string   `json:"Unpack"`
	Version  string   `json:"Version"`
}

type androidIntelV3Index struct {
	Artifacts []androidIntelV3Artifact `json:"Artifacts"`
}

var (
	androidGeoIPHashMu sync.RWMutex
	androidGeoIPHashes = map[string]string{}
)

// AndroidGeoIPExpectedSHA256 returns the SHA-256 published by Safing's current
// Intel index for the unpacked MMDB corresponding to a legacy resource ID.
func AndroidGeoIPExpectedSHA256(resource string) string {
	identifier := strings.TrimPrefix(resource, "all/")
	androidGeoIPHashMu.RLock()
	hash := androidGeoIPHashes[identifier]
	androidGeoIPHashMu.RUnlock()
	return hash
}

func setAndroidGeoIPExpectedSHA256(resource, digest string) {
	identifier := strings.TrimPrefix(resource, "all/")
	androidGeoIPHashMu.Lock()
	androidGeoIPHashes[identifier] = strings.ToLower(digest)
	androidGeoIPHashMu.Unlock()
}

// injectAndroidGeoIPCompat bridges the current Safing v3 Intel index into the
// legacy updater identifiers used by Portmaster/SPN 2023. The actual unpacked
// file hash is verified by the Android geoip compatibility hook before MMDB is
// opened.
func injectAndroidGeoIPCompat(reg *updater.ResourceRegistry) error {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(androidIntelV3URL)
	if err != nil {
		return fmt.Errorf("fetch current Safing intel index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch current Safing intel index: HTTP %s", resp.Status)
	}

	var index androidIntelV3Index
	decoder := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 2<<20))
	if err := decoder.Decode(&index); err != nil {
		return fmt.Errorf("decode current Safing intel index: %w", err)
	}

	wanted := map[string]string{
		"geoipv4.mmdb": "all/intel/geoip/geoipv4.mmdb.gz",
		"geoipv6.mmdb": "all/intel/geoip/geoipv6.mmdb.gz",
	}

	compatIndex := &updater.Index{
		Path:         "android-intel-v3-compat",
		Channel:      "android-intel-v3-compat",
		AutoDownload: true,
	}

	found := 0
	for _, artifact := range index.Artifacts {
		identifier, ok := wanted[strings.TrimSpace(artifact.Filename)]
		if !ok {
			continue
		}

		version := strings.TrimSpace(artifact.Version)
		digest := strings.TrimSpace(artifact.SHA256)
		if version == "" {
			return fmt.Errorf("Safing intel artifact %s has no version", artifact.Filename)
		}
		decodedDigest, err := hex.DecodeString(digest)
		if err != nil || len(decodedDigest) != 32 {
			return fmt.Errorf("Safing intel artifact %s has invalid SHA-256", artifact.Filename)
		}
		if artifact.Unpack != "gz" {
			return fmt.Errorf("Safing intel artifact %s changed unpack format to %q", artifact.Filename, artifact.Unpack)
		}
		if len(artifact.URLs) == 0 {
			return fmt.Errorf("Safing intel artifact %s has no download URL", artifact.Filename)
		}
		for _, artifactURL := range artifact.URLs {
			if !strings.HasPrefix(artifactURL, "https://updates.safing.io/") {
				return fmt.Errorf("Safing intel artifact %s contains unexpected URL %q", artifact.Filename, artifactURL)
			}
		}

		setAndroidGeoIPExpectedSHA256(identifier, digest)
		if err := reg.AddResource(identifier, version, compatIndex, false, true, false); err != nil {
			return fmt.Errorf("register Android compat resource %s: %w", identifier, err)
		}
		found++
	}

	if found != len(wanted) {
		return fmt.Errorf("current Safing intel index exposed %d/%d required GeoIP artifacts", found, len(wanted))
	}
	return nil
}
