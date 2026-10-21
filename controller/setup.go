package controller

import (
	"net/http"
	"strings"

	"github.com/infinmalum/one-gateway/common"
	"github.com/infinmalum/one-gateway/model"

	"github.com/gin-gonic/gin"
)

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// Setup creates the first root account. It only answers while the database
// has no users, so an exposed instance can never have its credentials
// replaced through it; afterwards the ordinary login and user management
// flows take over.
func Setup(c *gin.Context) {
	initialized, err := model.HasAnyUser()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无法读取用户数据，请稍后再试"})
		return
	}
	if initialized {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "系统已初始化，请直接登录"})
		return
	}
	var request setupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无效的请求内容"})
		return
	}
	username := strings.TrimSpace(request.Username)
	if err := common.Validate.Var(username, "required,max=12"); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "管理员用户名不能为空且不超过 12 个字符"})
		return
	}
	// Same strength bar as password reset, so a weak bootstrap password
	// cannot linger on the most privileged account.
	if err := common.Validate.Var(request.Password, "required,min=8,max=20"); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "密码长度需在 8-20 位之间"})
		return
	}
	email := strings.TrimSpace(request.Email)
	if email != "" {
		if err := common.Validate.Var(email, "email,max=50"); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "邮箱格式不正确"})
			return
		}
	}
	hashedPassword, err := common.Password2Hash(request.Password)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "密码处理失败，请稍后再试"})
		return
	}
	root := model.User{
		Username:    username,
		Password:    hashedPassword,
		Role:        model.RoleRootUser,
		Status:      model.UserStatusEnabled,
		DisplayName: username,
		Email:       email,
		Quota:       500000000000000,
	}
	if err := model.DB.Create(&root).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "创建管理员账户失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
