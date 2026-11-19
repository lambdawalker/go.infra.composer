package dnsprovider

var cloudflare = Provider{
	ID:       "cloudflare",
	Name:     "Cloudflare",
	Guidance: "Open the domain's DNS records in Cloudflare. Set CNAME records used for domain verification or DKIM to DNS only.",
	aliases:  []string{"cloud flare"},
}
