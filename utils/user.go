package utils

import (
	"fmt"
	"regexp"
	"strings"

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

// SanitizeString removes non-alphanumeric characters and converts to lowercase
func SanitizeString(input string) string {
	// Remove non-alphanumeric characters and convert to lowercase
	reg := regexp.MustCompile("[^a-zA-Z0-9]+")
	return strings.ToLower(reg.ReplaceAllString(input, ""))
}
