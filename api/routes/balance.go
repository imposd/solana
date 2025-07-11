package routes

import (
	"strings"

	"nova/api/services"
	"nova/api/types"

	"github.com/gofiber/fiber/v2"
)

var solanaService *services.SolanaService

func InitSolanaService(service *services.SolanaService) {
	solanaService = service
}

func GetBalance(ctx *fiber.Ctx) error {
	var request types.BalanceRequest
	if err := ctx.BodyParser(&request); err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(types.ErrorResponse{
			Success: false,
			Message: "Invalid request body",
		})
	}

	if len(request.Wallets) == 0 {
		return ctx.Status(fiber.StatusBadRequest).JSON(types.ErrorResponse{
			Success: false,
			Message: "No wallets provided",
		})
	}

	if len(request.Wallets) > 100 {
		return ctx.Status(fiber.StatusBadRequest).JSON(types.ErrorResponse{
			Success: false,
			Message: "Too many wallets (max 100)",
		})
	}

	validWallets := make([]string, 0, len(request.Wallets))
	for _, wallet := range request.Wallets {
		wallet = strings.TrimSpace(wallet)
		if wallet != "" {
			validWallets = append(validWallets, wallet)
		}
	}

	if len(validWallets) == 0 {
		return ctx.Status(fiber.StatusBadRequest).JSON(types.ErrorResponse{
			Success: false,
			Message: "No valid wallets provided",
		})
	}

	results := solanaService.GetMultipleBalances(validWallets)

	return ctx.JSON(types.BalanceResponse{
		Success: true,
		Data:    results,
	})
}
