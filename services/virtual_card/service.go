package virtualcard

import (
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/flutterwave"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

// VirtualCardService handles all virtual card business logic
type VirtualCardService struct {
	store          *db.Store
	flutterwaveAPI *flutterwave.Client
	walletService  *wallet.WalletService
	// notificationSvc NotificationService
	logger *logging.Logger
	config *utils.Config
}
