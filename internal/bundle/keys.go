package bundle

// TrustedKeys are the minisign public keys whose bundles Theme Tender
// accepts: the base64 line of each public key file. It is empty until the
// owner creates the release signing key (docs/runbooks/bundle-signing.md);
// until then every downloaded bundle is rejected and the built-in bundle
// is used.
var TrustedKeys = []string{}
