// Package http serves the REST API of the subscription service.
package http

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/usecase"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/dto"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/transport/http/middleware"
)

const (
	timeoutSubscribe = 25 * time.Second
	timeoutDefault   = 3 * time.Second

	maxEmailLen = 254
)

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)

// Handler serves the subscription endpoints. It reports failures with middleware.Fail and never picks a status code.
type Handler struct {
	subscriptions usecase.SubscriptionUseCase
}

// NewHandler returns a Handler.
func NewHandler(subscriptions usecase.SubscriptionUseCase) *Handler {
	return &Handler{subscriptions: subscriptions}
}

// Subscribe handles POST /api/v1/subscribe.
func (h *Handler) Subscribe(c *gin.Context) {
	var req dto.SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, middleware.BodyError{Err: err})
		return
	}
	email, repo := strings.TrimSpace(req.Email), strings.TrimSpace(req.Repository)
	switch {
	case email == "":
		middleware.Fail(c, middleware.BodyError{Err: model.ErrEmailRequired})
		return
	case repo == "":
		middleware.Fail(c, middleware.BodyError{Err: errors.New("repository is required")})
		return
	}
	if err := validateSubscribe(email, repo); err != nil {
		middleware.Fail(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), timeoutSubscribe)
	defer cancel()

	if err := h.subscriptions.Subscribe(ctx, email, repo); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(
		http.StatusAccepted,
		dto.MessageResponse{Message: "Subscription initiated. Please check your email to confirm."},
	)
}

// Confirm handles GET /api/v1/confirm/:token.
func (h *Handler) Confirm(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeoutDefault)
	defer cancel()

	if err := h.subscriptions.Confirm(ctx, c.Param("token")); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "subscription confirmed successfully"})
}

// List handles GET /api/v1/subscriptions.
func (h *Handler) List(c *gin.Context) {
	email := strings.TrimSpace(c.Query("email"))
	if email == "" {
		middleware.Fail(c, model.ErrEmailRequired)
		return
	}
	if !validEmail(email) {
		middleware.Fail(c, model.ErrInvalidEmailFormat)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), timeoutDefault)
	defer cancel()

	subs, err := h.subscriptions.List(ctx, email)
	if err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.NewListResponse(subs))
}

// Unsubscribe handles GET /api/v1/unsubscribe/:token.
func (h *Handler) Unsubscribe(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeoutDefault)
	defer cancel()

	if err := h.subscriptions.Unsubscribe(ctx, c.Param("token")); err != nil {
		middleware.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.MessageResponse{Message: "You have been successfully unsubscribed"})
}

func validateSubscribe(email, repo string) error {
	var errs []error
	if !validEmail(email) {
		errs = append(errs, model.ErrInvalidEmailFormat)
	}
	if !repoPattern.MatchString(repo) {
		errs = append(errs, model.ErrInvalidRepoFormat)
	}
	if len(errs) > 0 {
		return middleware.ValidationError{Errs: errs}
	}
	return nil
}

// validEmail accepts a bare RFC 5322 address; the "Name <a@b.c>" form is rejected.
func validEmail(email string) bool {
	if len(email) > maxEmailLen {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}
