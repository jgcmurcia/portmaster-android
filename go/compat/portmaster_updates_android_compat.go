package updates

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/safing/portbase/updater"
)

const androidIntelV3URL = "https://updates.safing.io/intel.v3.json"

type androidIntelV3Index struct {
	Artifacts []struct {
		Filename string
		Version  string
	} `json:"Artifacts"`
}

// injectAndroidGeoIPCompat bridges the current Safing v3 Intel index into the
// legacy updater identifiers used by Portmaster/SPN 2023.
//
// Safing still hosts the versioned .mmdb.gz files at the exact URL that the
// legacy updater generates. Only the old intel.json entries were removed.
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
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
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
		if version == "" {
			return fmt.Errorf("Safing intel artifact %s has no version", artifact.Filename)
		}
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
