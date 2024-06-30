package utils

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

func GetActiveUser(ctx *gin.Context) (int64, error) {
	value, exists := ctx.Get("user_id")
	if !exists {
		return 0, fmt.Errorf("error occurred, not authorized to access this resource")
	}

	userId, ok := value.(int64)
	if !ok {
		return 0, fmt.Errorf("an error occurred")
	}

	return userId, nil
}
