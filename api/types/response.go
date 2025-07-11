package types

type BalanceResponse struct {
	Success bool            `json:"success"`
	Data    []WalletBalance `json:"data"`
	Message string          `json:"message,omitempty"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
