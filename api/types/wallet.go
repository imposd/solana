package types

type BalanceRequest struct {
	Wallets []string `json:"wallets" validate:"required"`
}

type WalletBalance struct {
	Address string  `json:"address"`
	Balance float64 `json:"balance"`
	Error   string  `json:"error,omitempty"`
}
