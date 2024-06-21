package api

import (
	"context"
	"database/sql"
	"net/http"

	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func (a *Auth) login(ctx *gin.Context) {
	user := new(models.UserLoginParams)

	if err := ctx.ShouldBindJSON(user); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dbUser, err := a.server.queries.GetUserByEmail(context.Background(), user.Email)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect Email or Password"})
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err = utils.VerifyHashValue(user.Password, dbUser.HashedPassword.String); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect Email or Password"})
		return
	}

	token, err := TokenController.CreateToken(dbUser.ID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"token": token})
}
