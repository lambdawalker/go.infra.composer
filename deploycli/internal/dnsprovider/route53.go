package dnsprovider

var route53 = Provider{
	ID:       "route53",
	Name:     "AWS Route 53",
	Guidance: "Add public DNS records in the Route 53 hosted zone whose name servers are delegated for the domain.",
	aliases:  []string{"aws", "route 53"},
}
