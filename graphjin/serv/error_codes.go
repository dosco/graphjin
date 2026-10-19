package serv

// Codes GraphJin puts in a GraphQL error's extensions.code. They are part of
// the public contract: clients match them instead of the message text.
const (
	// ErrorCodeKeystoreKeyRequired: a request sets or reads an encrypted
	// secret, and secrets.keystore.key is not set.
	ErrorCodeKeystoreKeyRequired = "keystore_key_required"
)

// KeystoreKeyRequiredMessage begins the message of a keystore_key_required
// error. Config results that report errors as text (errors_json) carry the
// message without the code.
const KeystoreKeyRequiredMessage = "secrets.keystore.key must be set"
