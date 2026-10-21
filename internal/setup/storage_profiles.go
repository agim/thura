package setup

import (
	"errors"
	"net/url"
	"strings"
)

// Keep the storage pack's S3 backend and existing locations intact. Profiles
// derive connection settings; they never create buckets or migrate objects.
func storageLocations(profile string) []string {
	switch profile {
	case "aws":
		return strings.Fields("us-east-1 us-east-2 us-west-1 us-west-2 af-south-1 ap-east-1 ap-east-2 ap-south-1 ap-south-2 ap-southeast-1 ap-southeast-2 ap-southeast-3 ap-southeast-4 ap-southeast-5 ap-southeast-6 ap-southeast-7 ap-northeast-1 ap-northeast-2 ap-northeast-3 ca-central-1 ca-west-1 eu-central-1 eu-central-2 eu-west-1 eu-west-2 eu-west-3 eu-south-1 eu-south-2 eu-north-1 il-central-1 me-south-1 me-central-1 mx-central-1 sa-east-1")
	case "digitalocean":
		return strings.Fields("nyc3 ams3 sfo2 sfo3 sgp1 lon1 fra1 tor1 blr1 syd1 atl1 ric1 mkc1")
	}
	return nil
}

func profileEndpoint(v map[string]string) (string, string, string, error) {
	location := v["THURA_STORAGE_LOCATION"]
	switch v["THURA_STORAGE_PROFILE"] {
	case "", "custom":
		return v["STORAGE_ENDPOINT"], v["STORAGE_REGION"], v["STORAGE_PATH_STYLE"], nil
	case "google":
		return "https://storage.googleapis.com", "auto", "", nil
	case "aws", "digitalocean":
		valid := false
		for _, region := range storageLocations(v["THURA_STORAGE_PROFILE"]) {
			valid = valid || location == region
		}
		if !valid {
			return "", "", "", errors.New("select a region for the storage provider")
		}
		if v["THURA_STORAGE_PROFILE"] == "digitalocean" {
			return "https://" + location + ".digitaloceanspaces.com", "us-east-1", "", nil
		}
		// The storage pack selects AWS virtual hosting and other providers’ path addressing.
		return "https://s3." + location + ".amazonaws.com", location, "", nil
	}
	return "", "", "", errors.New("choose a supported storage profile")
}

func storageVendor(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "s3.amazonaws.com" || strings.HasPrefix(host, "s3.") && strings.HasSuffix(host, ".amazonaws.com") {
		return "aws"
	}
	if strings.HasSuffix(host, ".digitaloceanspaces.com") {
		return "digitalocean"
	}
	if host == "storage.googleapis.com" {
		return "google"
	}
	return ""
}

func configureStorageProfile(v, previous map[string]string, submitted map[string][]string) error {
	if v["STORAGE_PROVIDER"] != "s3" {
		return nil
	}
	endpoint, region, pathStyle, err := profileEndpoint(v)
	if err != nil {
		return err
	}
	oldEndpoint := previous["STORAGE_ENDPOINT"]
	// Never send another provider's saved key to a newly selected host. An
	// explicitly supplied replacement key/secret must both be present.
	sameVendor := storageVendor(oldEndpoint) != "" && storageVendor(oldEndpoint) == storageVendor(endpoint)
	if oldEndpoint != endpoint && !sameVendor {
		for _, name := range []string{"STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY"} {
			if len(submitted[name]) == 0 || strings.TrimSpace(submitted[name][0]) == "" {
				v[name] = ""
			}
		}
	}
	v["STORAGE_ENDPOINT"], v["STORAGE_REGION"], v["STORAGE_PATH_STYLE"] = endpoint, region, pathStyle
	return nil
}

func validateStorageProfile(v map[string]string) error {
	if v["STORAGE_PROVIDER"] != "s3" {
		return nil
	}
	endpoint, region, pathStyle, err := profileEndpoint(v)
	if err != nil {
		return err
	}
	if endpoint != v["STORAGE_ENDPOINT"] || region != v["STORAGE_REGION"] || pathStyle != v["STORAGE_PATH_STYLE"] {
		return errors.New("save the storage profile to apply its endpoint and signing settings")
	}
	return nil
}

func (f Field) OptionLabel(value string) string {
	if f.Name == "THURA_STORAGE_PROFILE" {
		switch value {
		case "aws":
			return "AWS S3"
		case "digitalocean":
			return "DigitalOcean Spaces"
		case "google":
			return "Google Cloud Storage"
		case "custom":
			return "Custom S3-compatible"
		}
	}
	if f.Name == "STORAGE_PROVIDER" {
		if value == "s3" {
			return "Cloud object storage (S3-compatible)"
		}
		if value == "local" {
			return "Local disk"
		}
	}
	return value
}

func (f Field) OptionGroup(value string) string {
	if f.Name != "THURA_STORAGE_LOCATION" {
		return ""
	}
	for _, profile := range []string{"aws", "digitalocean"} {
		for _, region := range storageLocations(profile) {
			if region == value {
				return profile
			}
		}
	}
	return ""
}
