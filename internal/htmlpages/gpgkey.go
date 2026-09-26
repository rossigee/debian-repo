// Package htmlpages provides HTML rendering for repository pages.
package htmlpages

import (
	"fmt"
	"html"

	"git.golder.lan/rossgolderltd/debian-repo/internal/gpgsign"
)

// RenderGPGKeyHTML generates the GPG key information page
func RenderGPGKeyHTML(keyInfo *gpgsign.KeyInfo, repoURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<title>GPG Key Information</title>
<link rel="stylesheet" href="/static/styles.css">
<style>
.key-details {
  background: linear-gradient(135deg, #dbeafe 0%%, #bfdbfe 100%%);
  border: 2px solid #3b82f6;
  border-radius: 8px;
  padding: 20px;
  margin: 25px 0;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.15);
}
.key-details strong {
  color: #1e40af;
  font-weight: 700;
}
.key-details code {
  background: #eff6ff;
  color: #1e3a8a;
  border: 1px solid #3b82f6;
  padding: 4px 8px;
  margin: 0 2px;
}
.uids {
  margin: 12px 0 0 0;
  padding: 10px 0 0 0;
}
.uids > div {
  margin: 6px 0;
  padding-left: 20px;
  position: relative;
}
.uids > div:before {
  content: "•";
  position: absolute;
  left: 0;
  color: #3b82f6;
  font-weight: bold;
}
</style>
</head>
<body>
`+"`+NavBar(\"gpg\")+`"+`
<header>
<h1>Repository GPG Key</h1>
</header>

<main>

<div class="key-details">
<strong>Key Information:</strong><br>
<br>
<strong>Key ID:</strong> <code>%s</code><br>
<strong>Fingerprint:</strong> <code>%s</code><br>
<br>
<strong>User IDs:</strong>
<div class="uids">
%s
</div>
</div>

<h2>Public Key Block</h2>

<pre>%s</pre>

<h2>Import Key</h2>

<pre>
# Download the key
curl -O %s/pubkey.gpg

# Import into GPG
gpg --import pubkey.gpg

# Or use directly with apt
curl %s/pubkey.gpg | sudo gpg --dearmor -o /usr/share/keyrings/golder-tech.gpg
</pre>

</main>

<footer>
<p>Debian Repository • Powered by <a href="https://git.golder.lan/rossgolderltd/debian-repo">debian-repo</a></p>
</footer>

</body>
</html>
`, html.EscapeString(keyInfo.KeyID), html.EscapeString(keyInfo.Fingerprint), formatUIDs(keyInfo.UIDs), html.EscapeString(keyInfo.ArmoredPublicKey), repoURL, repoURL)
}

func formatUIDs(uids []string) string {
	result := ""
	for _, uid := range uids {
		result += fmt.Sprintf("&nbsp;&nbsp;&bull; <code>%s</code><br>\n", html.EscapeString(uid))
	}
	return result
}
