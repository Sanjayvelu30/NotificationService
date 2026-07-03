package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sanjay/NotificationService/internal/repository"
)

type TemplateHandler struct {
	repo repository.TemplateRepo
}

func NewTemplateHandler(repo repository.TemplateRepo) *TemplateHandler {
	return &TemplateHandler{repo: repo}
}

type saveTemplateRequest struct {
	Name string `json:"name" binding:"required"`
	Body string `json:"body" binding:"required"`
}

func (h *TemplateHandler) Save(c *gin.Context) {
	var req saveTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	name := strings.ToUpper(strings.TrimSpace(req.Name))
	body := strings.TrimSpace(req.Body)

	if name == "" || body == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and body cannot be empty"})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	if err := h.repo.Save(name, body, userIDStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "template saved successfully",
		"name":    name,
		"body":    body,
	})
}

func (h *TemplateHandler) GetAll(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	templates, err := h.repo.GetAll(userIDStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, templates)
}
