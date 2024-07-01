package utils

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

func GetActiveUser(ctx *gin.Context) (TokenObject, error) {
	value, exists := ctx.Get("user")
	if !exists {
		return TokenObject{}, fmt.Errorf("error occurred, not authorized to access this resource")
	}

	user, ok := value.(TokenObject)
	if !ok {
		return TokenObject{}, fmt.Errorf("an error occurred")
	}

	return user, nil
}
