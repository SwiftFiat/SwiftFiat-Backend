package cryptocurrency

type WalletCreationModel struct {
	Label                           string `json:"label"`                           // The label for the wallet
	Passphrase                      string `json:"passphrase"`                      // The wallet's passphrase
	Enterprise                      string `json:"enterprise"`                      // Enterprise ID associated with the wallet
	DisableTransactionNotifications bool   `json:"disableTransactionNotifications"` // Flag to disable transaction notifications
	DisableKRSEmail                 bool   `json:"disableKRSEmail"`                 // Flag to disable KRS email notifications
}
