package network

import (
	"os"
	"runtime"
	"strings"

	_ "golang.org/x/crypto/x509roots/fallback"
)

var standardLinuxCertFiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
	"/etc/ssl/ca-bundle.pem",
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem",
	"/etc/ssl/cert.pem",
}

var androidCertDirs = []string{
	"/apex/com.android.conscrypt/cacerts",
	"/system/etc/security/cacerts",
	"/data/misc/user/0/cacerts-added",
	"/data/misc/keychain/certs-added",
	"/data/data/com.termux/files/usr/etc/tls",
}

// InitTLSRootCAs ensures Go and child processes on Linux/Android can locate system CA directories
// before crypto/x509 runs its lazy one-time system root initialization.
// Blank-importing x509roots/fallback also embeds Mozilla root CAs when OS roots are unavailable.
func InitTLSRootCAs() {
	initTLSRootCAsWithPaths(runtime.GOOS, standardLinuxCertFiles, androidCertDirs)
}

func initTLSRootCAsWithPaths(goos string, certFiles, certDirs []string) {
	if goos != "linux" {
		return
	}
	if strings.TrimSpace(os.Getenv("SSL_CERT_FILE")) != "" || strings.TrimSpace(os.Getenv("SSL_CERT_DIR")) != "" {
		return
	}
	for _, f := range certFiles {
		if info, err := os.Stat(f); err == nil && !info.IsDir() {
			return
		}
	}
	if found := selectExistingCertDirs(certDirs); len(found) > 0 {
		_ = os.Setenv("SSL_CERT_DIR", strings.Join(found, ":"))
	}
}

func selectExistingCertDirs(candidates []string) []string {
	var found []string
	for _, dir := range candidates {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) == 0 {
			continue
		}
		found = append(found, dir)
	}
	return found
}
