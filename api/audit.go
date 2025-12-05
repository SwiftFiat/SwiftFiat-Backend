package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/gin-gonic/gin"
)

type AuditHandler struct {
	server  *Server
	service *audit.Service
}

func (a AuditHandler) router(server *Server) {
	a.server = server
	a.service = server.auditService

	audit := server.router.Group("/api/v1/audit")
	audit.Use(a.server.authMiddleware.AuthenticatedMiddleware())
	audit.GET("/logs/{id}", a.GetLogByID)
	// audit.GET("/logs", a.SearchLogs)
	audit.GET("/user/{userID}/activity", a.GetUserActivity)
	audit.GET("/entity/{entityType}/{entityID}/history", a.GetEntityHistory)

	// Analytics endpoints
	audit.GET("/stats", a.GetStats)
	audit.GET("/critical", a.GetRecentCritical)
	audit.GET("/suspicious", a.GetSuspiciousActivity)
	audit.GET("/categories", a.GetCategoryBreakdown)

	// Compliance endpoints
	// audit.GET("/compliance/export", a.ExportLogs)
	audit.GET("/compliance/user-data/{userID}", a.GetUserData)

}

// GetLogByID godoc
// @Summary      Get Audit Log by ID
// @Description  Retrieves a specific audit log entry by its ID.
// @Tags         Audit
// @Produce      json
// @Param        id   path      int  true  "Audit Log ID"
// @Success      200  {object}  basemodels.SuccessResponse{data=audit.LogResponse}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      404  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/logs/{id} [get]
func (h *AuditHandler) GetLogByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, basemodels.NewError("Invalid log ID"))
		return
	}

	log, err := h.service.GetByID(c, id)
	if err != nil {
		c.JSON(404, basemodels.NewError("audit log not found"))
		return
	}

	c.JSON(200, basemodels.NewSuccess("", log))
}

// GetUserActivity godoc
// @Summary      Get User Activity Timeline
// @Description  Retrieves the activity timeline for a specific user within a date range.
// @Tags         Audit
// @Produce      json
// @Param        userID     path      int     true  "User ID"
// @Param        start_date query     string  false "Start Date (YYYY-MM-DD or RFC3339)"
// @Param        end_date   query     string  false "End Date (YYYY-MM-DD or RFC3339)"
// @Param        limit      query     int     false "Limit number of records" default(50)
// @Param        offset     query     int     false "Offset for pagination" default(0)
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/user/{userID}/activity [get]
func (h *AuditHandler) GetUserActivity(c *gin.Context) {
	userIDStr := c.Param("userID")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid user ID"))
		return
	}
	query := c.Request.URL.Query()
	startDate, err := h.parseDate(query.Get("start_date"), time.Now().AddDate(0, -1, 0))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid start_date"))
		return
	}

	endDate, err := h.parseDate(query.Get("end_date"), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid end_date"))
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	logs, err := h.service.GetUserActivity(c, int64(userID), startDate, endDate, int32(limit), int32(offset))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to retrieve user activity"))
		return
	}

	response := map[string]interface{}{
		"user_id":  userID,
		"activity": logs,
		"count":    len(logs),
		"period": map[string]string{
			"start": startDate.Format("2006-01-02"),
			"end":   endDate.Format("2006-01-02"),
		},
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", response))
}

// GetStats godoc
// @Summary      Get Audit Statistics
// @Description  Retrieves aggregated audit statistics for a specified date range.
// @Tags         Audit
// @Produce      json
// @Param        start_date query     string  false "Start Date (YYYY-MM-DD or RFC3339)"
// @Param        end_date   query     string  false "End Date (YYYY-MM-DD or RFC3339)"
// @Success      200  {object}  basemodels.SuccessResponse{data=audit.AuditStats}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/stats [get]
func (h *AuditHandler) GetStats(c *gin.Context) {
	query := c.Request.URL.Query()

	startDate, err := h.parseDate(query.Get("start_date"), time.Now().AddDate(0, -1, 0))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid start_date"))
		return
	}

	endDate, err := h.parseDate(query.Get("end_date"), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid end_date"))
		return
	}

	stats, err := h.service.GetStats(c, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to retrieve audit stats"))
		return
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", stats))
}

// GetEntityHistory godoc
// @Summary      Get Entity Change History
// @Description  Retrieves the change history for a specific entity type and ID.
// @Tags         Audit
// @Produce      json
// @Param        entityType   path      string  true  "Entity Type"
// @Param        entityID     path      string  true  "Entity ID"
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/entity/{entityType}/{entityID}/history [get]
func (h *AuditHandler) GetEntityHistory(c *gin.Context) {
	entityType := c.Param("entityType")
	entityID := c.Param("entityID")

	if entityType == "" || entityID == "" {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Entity type and ID are required"))
		return
	}

	history, err := h.service.GetEntityHistory(c, entityType, entityID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to retrieve entity history"))
		return
	}

	response := map[string]interface{}{
		"entity_type": entityType,
		"entity_id":   entityID,
		"history":     history,
		"changes":     len(history),
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", response))
}

// GetRecentCritical godoc
// @Summary      Get Recent Critical Audit Events
// @Description  Retrieves recent critical audit events.
// @Tags         Audit
// @Produce      json
// @Param        limit  query     int  false  "Limit number of records" default(100)
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/critical [get]
func (h *AuditHandler) GetRecentCritical(c *gin.Context) {
	limit := 100
	query := c.Request.URL.Query()
	if lStr := query.Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	logs, err := h.service.GetRecentCritical(c, int32(limit))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to retrieve critical audit logs"))
		return
	}

	response := map[string]interface{}{
		"critical_events": logs,
		"count":           len(logs),
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", response))
}

// GetSuspiciousActivity godoc
// @Summary      Get Suspicious Audit Activities
// @Description  Identifies and retrieves potentially suspicious audit activities.
// @Tags         Audit
// @Produce      json
// @Param        since       query     string  false "Since duration (e.g., '24h', '7d')" default("24h")
// @Param        min_events  query     int     false "Minimum number of events to consider suspicious" default(5)
// @Param        limit       query     int     false "Limit number of records" default(100)
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/suspicious [get]
func (h *AuditHandler) GetSuspiciousActivity(c *gin.Context) {
	query := c.Request.URL.Query()

	// Parse "since" duration (e.g., "24h", "7d")
	sinceDuration := query.Get("since")
	if sinceDuration == "" {
		sinceDuration = "24h"
	}

	duration, err := time.ParseDuration(sinceDuration)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid duration format. Use format like '24h' or '7d'"))
		return
	}

	sinceDate := time.Now().Add(-duration)

	// Parse min_events threshold
	minEvents := int32(5) // Default
	if minStr := query.Get("min_events"); minStr != "" {
		if min, err := strconv.ParseInt(minStr, 10, 32); err == nil && min > 0 {
			minEvents = int32(min)
		}
	}

	limit := 100
	if lStr := query.Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	activities, err := h.service.GetSuspiciousActivity(c, sinceDate, minEvents, int32(limit))
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to retrieve suspicious activities"))
		return
	}

	response := map[string]interface{}{
		"suspicious_activities": activities,
		"count":                 len(activities),
		"criteria": map[string]interface{}{
			"since":      sinceDate.Format(time.RFC3339),
			"min_events": minEvents,
		},
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", response))
}

// GetCategoryBreakdown godoc
// @Summary      Get Audit Category Breakdown
// @Description  Retrieves a breakdown of audit logs by category within a specified date range.
// @Tags         Audit
// @Produce      json
// @Param        start_date query     string  false "Start Date (YYYY-MM-DD or RFC3339)"
// @Param        end_date   query     string  false "End Date (YYYY-MM-DD or RFC3339)"
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/categories [get]
func (h *AuditHandler) GetCategoryBreakdown(c *gin.Context) {
	query := c.Request.URL.Query()

	startDate, err := h.parseDate(query.Get("start_date"), time.Now().AddDate(0, -1, 0))
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid start_date"))
		return
	}

	endDate, err := h.parseDate(query.Get("end_date"), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid end_date"))
		return
	}

	// This would need corresponding methods in service
	// For now, return a structured response
	response := map[string]interface{}{
		"period": map[string]string{
			"start": startDate.Format("2006-01-02"),
			"end":   endDate.Format("2006-01-02"),
		},
		"message": "Category breakdown endpoint",
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", response))
}

// GetUserData godoc
// @Summary      Get User Data Export
// @Description  Exports all audit logs related to a specific user for compliance purposes.
// @Tags         Audit
// @Produce      json
// @Param        userID     path      int     true  "User ID"
// @Success      200  {object}  basemodels.SuccessResponse{data=map[string]interface{}}
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/compliance/user-data/{userID} [get]
func (h *AuditHandler) GetUserData(c *gin.Context) {
	userIDStr := c.Param("userID")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid user ID"))
		return
	}

	// Get all time data for the user
	startDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Now()

	logs, err := h.service.GetUserActivity(c, int64(userID), startDate, endDate, 10000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to retrieve user data"))
		return
	}

	response := map[string]interface{}{
		"user_id":     userID,
		"data_points": len(logs),
		"audit_logs":  logs,
		"exported_at": time.Now().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, basemodels.NewSuccess("", response))
}

// ExportLogs godoc
// @Summary      Export Audit Logs
// @Description  Exports audit logs within a specified date range in JSON or CSV format.
// @Tags         Audit
// @Produce      json
// @Param        format      query     string  false "Export format: 'json' or 'csv'" default("json")
// @Param        start_date  query     string  false "Start Date (YYYY-MM-DD or RFC3339)"
// @Param        end_date    query     string  false "End Date (YYYY-MM-DD or RFC3339)"
// @Success      200  {file}    file
// @Failure      400  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/audit/compliance/export [get]
// func (h *AuditHandler) ExportLogs(c *gin.Context) {
// 	query := c.Request.URL.Query()
// 	format := query.Get("format")
// 	if format == "" {
// 		format = "json"
// 	}

// 	startDate, err := h.parseDate(query.Get("start_date"), time.Now().AddDate(0, -1, 0))
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid start_date"))
// 		return
// 	}

// 	endDate, err := h.parseDate(query.Get("end_date"), time.Now())
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid end_date"))
// 		return
// 	}

// 	filters := audit.SearchFilters{
// 		StartDate: startDate,
// 		EndDate:   endDate,
// 		Limit:     10000, // Large limit for export
// 		Offset:    0,
// 	}

// 	logs, err := h.service.Search(c, filters)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to export audit logs"))
// 		return
// 	}

// 	switch format {
// 	case "csv":
// 		h.exportCSV(c, logs)
// 	case "json":
// 		h.exportJSON(c, logs)
// 	default:
// 		c.JSON(http.StatusBadRequest, basemodels.NewError("Invalid format. Use 'json' or 'csv'"))
// 	}
// }

func (h *AuditHandler) exportCSV(c *gin.Context, logs []audit.LogResponse) {
	w := c.Writer
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=audit_logs.csv")
	

	// Write CSV header
	w.Write([]byte("ID,Timestamp,Category,Event Type,Severity,Actor ID,Actor Email,Entity Type,Entity ID,Action,Description,Success,IP Address\n"))

	// Write data rows
	for _, log := range logs {
		actorID := ""
		if log.ActorID != nil {
			actorID = strconv.FormatInt(*log.ActorID, 10)
		}

		actorEmail := ""
		if log.ActorEmail != nil {
			actorEmail = *log.ActorEmail
		}

		ipAddr := ""
		if log.IPAddress != nil {
			ipAddr = *log.IPAddress
		}

		row := []string{
			strconv.FormatInt(log.ID, 10),
			log.CreatedAt.Format(time.RFC3339),
			string(log.EventCategory),
			log.EventType,
			string(log.Severity),
			actorID,
			actorEmail,
			log.EntityType,
			log.EntityID,
			string(log.Action),
			log.Description,
			strconv.FormatBool(log.Success),
			ipAddr,
		}

		for i, field := range row {
			if i > 0 {
				w.Write([]byte(","))
			}
			w.Write([]byte("\"" + field + "\""))
		}
		w.Write([]byte("\n"))
	}
}

func (h *AuditHandler) exportJSON(c *gin.Context, logs []audit.LogResponse) {
	w := c.Writer
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=audit_logs.json")

	response := map[string]interface{}{
		"exported_at": time.Now().Format(time.RFC3339),
		"total_logs":  len(logs),
		"logs":        logs,
	}

	json.NewEncoder(w).Encode(response)
}

// func (h *AuditHandler) SearchLogs(w http.ResponseWriter, r *http.Request) {
// 	query := r.URL.Query()

// 	// Parse filters
// 	filters := audit.SearchFilters{
// 		Limit:  50,  // Default
// 		Offset: 0,
// 	}

// 	// Parse optional category
// 	if cat := query.Get("category"); cat != "" {
// 		category := audit.EventCategory(cat)
// 		filters.EventCategory = &category
// 	}

// 	// Parse optional event type
// 	if et := query.Get("event_type"); et != "" {
// 		filters.EventType = &et
// 	}

// 	// Parse optional severity
// 	if sev := query.Get("severity"); sev != "" {
// 		severity := audit.Severity(sev)
// 		filters.Severity = &severity
// 	}

// 	// Parse optional actor ID
// 	if actorStr := query.Get("actor_id"); actorStr != "" {
// 		if actorID, err := strconv.ParseInt(actorStr, 10, 64); err == nil {
// 			filters.ActorID = &actorID
// 		}
// 	}

// 	// Parse optional entity type
// 	if et := query.Get("entity_type"); et != "" {
// 		filters.EntityType = &et
// 	}

// 	// Parse optional entity ID
// 	if eid := query.Get("entity_id"); eid != "" {
// 		filters.EntityID = &eid
// 	}

// 	// Parse date range
// 	startDate, err := h.parseDate(query.Get("start_date"), time.Now().AddDate(0, -1, 0))
// 	if err != nil {
// 		h.respondError(w, http.StatusBadRequest, "Invalid start_date format. Use YYYY-MM-DD")
// 		return
// 	}
// 	filters.StartDate = startDate

// 	endDate, err := h.parseDate(query.Get("end_date"), time.Now())
// 	if err != nil {
// 		h.respondError(w, http.StatusBadRequest, "Invalid end_date format. Use YYYY-MM-DD")
// 		return
// 	}
// 	filters.EndDate = endDate

// 	// Parse pagination
// 	if limitStr := query.Get("limit"); limitStr != "" {
// 		if limit, err := strconv.ParseInt(limitStr, 10, 32); err == nil && limit > 0 && limit <= 100 {
// 			filters.Limit = int32(limit)
// 		}
// 	}

// 	if offsetStr := query.Get("offset"); offsetStr != "" {
// 		if offset, err := strconv.ParseInt(offsetStr, 10, 32); err == nil && offset >= 0 {
// 			filters.Offset = int32(offset)
// 		}
// 	}

// 	// Execute search
// 	logs, err := h.service.Search(r.Context(), filters)
// 	if err != nil {
// 		h.respondError(w, http.StatusInternalServerError, "Failed to search audit logs")
// 		return
// 	}

// 	response := map[string]interface{}{
// 		"logs":   logs,
// 		"total":  len(logs),
// 		"limit":  filters.Limit,
// 		"offset": filters.Offset,
// 	}

// 	h.respondJSON(w, http.StatusOK, response)
// }

func (h *AuditHandler) parseDate(dateStr string, defaultDate time.Time) (time.Time, error) {
	if dateStr == "" {
		return defaultDate, nil
	}

	// Try parsing as date (YYYY-MM-DD)
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		// Try parsing as datetime (RFC3339)
		t, err = time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return time.Time{}, err
		}
	}

	return t, nil
}
