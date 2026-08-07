package handler

import (
	"fmt"
	"net/http"

	"dify2api/db"
)

// R-A level-gated user-site endpoints (v1.3.0 S6).
//
// The level system is a user-site channel: 4 级 users get the donation
// application review panel, 5 级 users additionally get charity resource /
// pricing management and the site-wide request log (list + stats, no
// export). Entry happens on the user site via Discord login — the admin
// site login is never involved, and the existing /api/admin/* endpoints
// stay requireAdmin-only. Every new endpoint below starts with
// meLevelGuard; administrators pass through requireLevel unconditionally
// and keep all of their existing capabilities.

// meLevelGuard resolves the operator for an /api/me/... level-gated
// endpoint. It writes the unified error envelope itself (401 when there is
// no valid session, 403 when the effective level is below min) and returns
// nil when the request must stop.
func (g *Gateway) meLevelGuard(w http.ResponseWriter, r *http.Request, min int) *db.User {
	if u := g.requireLevel(r, min); u != nil {
		return u
	}
	if g.currentUser(r) == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return nil
	}
	g.writeError(w, http.StatusForbidden, "forbidden",
		fmt.Sprintf(t(g.resolveLang(r), "权限不足：需要用户等级 %d", "Insufficient privileges: user level %d required"), min))
	return nil
}

// --- Level-4: donation application review ---

// GET /api/me/review/pending — list pending applications (level >= 4).
func (g *Gateway) handleMeReviewPending(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 4) == nil {
		return
	}
	g.serveListPendingApplications(w, r)
}

// POST /api/me/review/{id}/approve — approve an application (level >= 4).
func (g *Gateway) handleMeReviewApprove(w http.ResponseWriter, r *http.Request) {
	u := g.meLevelGuard(w, r, 4)
	if u == nil {
		return
	}
	g.serveApproveApplication(w, r, u)
}

// POST /api/me/review/{id}/reject — reject an application (level >= 4).
func (g *Gateway) handleMeReviewReject(w http.ResponseWriter, r *http.Request) {
	u := g.meLevelGuard(w, r, 4)
	if u == nil {
		return
	}
	g.serveRejectApplication(w, r, u)
}

// POST /api/me/review/approve/batch — batch approve (level >= 4).
func (g *Gateway) handleMeReviewBatchApprove(w http.ResponseWriter, r *http.Request) {
	u := g.meLevelGuard(w, r, 4)
	if u == nil {
		return
	}
	g.serveBatchApproveApplications(w, r, u)
}

// POST /api/me/review/reject/batch — batch reject (level >= 4).
func (g *Gateway) handleMeReviewBatchReject(w http.ResponseWriter, r *http.Request) {
	u := g.meLevelGuard(w, r, 4)
	if u == nil {
		return
	}
	g.serveBatchRejectApplications(w, r, u)
}

// --- Level-5: charity co-admin (donation resources + pricing) ---

// GET /api/me/charity-admin/donations — list all donations (level 5).
func (g *Gateway) handleMeCharityListDonations(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveListDonations(w, r)
}

// POST /api/me/charity-admin/donations — create a donation (level 5).
func (g *Gateway) handleMeCharityCreateDonation(w http.ResponseWriter, r *http.Request) {
	u := g.meLevelGuard(w, r, 5)
	if u == nil {
		return
	}
	g.serveCreateDonation(w, r, u)
}

// PATCH /api/me/charity-admin/donations/{id} — update a donation (level 5).
func (g *Gateway) handleMeCharityPatchDonation(w http.ResponseWriter, r *http.Request) {
	u := g.meLevelGuard(w, r, 5)
	if u == nil {
		return
	}
	g.servePatchDonation(w, r, u)
}

// POST /api/me/charity-admin/donations/{id}/status — toggle status (level 5).
func (g *Gateway) handleMeCharityDonationStatus(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveDonationStatus(w, r)
}

// DELETE /api/me/charity-admin/donations/{id} — delete a donation (level 5).
func (g *Gateway) handleMeCharityDeleteDonation(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveDeleteDonation(w, r)
}

// POST /api/me/charity-admin/donations/status/batch — batch status (level 5).
func (g *Gateway) handleMeCharityBatchDonationStatus(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveBatchDonationStatus(w, r)
}

// POST /api/me/charity-admin/donations/delete/batch — batch delete (level 5).
func (g *Gateway) handleMeCharityBatchDeleteDonations(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveBatchDeleteDonations(w, r)
}

// GET /api/me/charity-admin/pricing — list pricing entries (level 5).
func (g *Gateway) handleMePricingList(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveListPricing(w, r)
}

// PUT /api/me/charity-admin/pricing — upsert a pricing entry (level 5).
func (g *Gateway) handleMePricingUpsert(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveUpsertPricing(w, r)
}

// PATCH /api/me/charity-admin/pricing — partial update (level 5).
func (g *Gateway) handleMePricingPatch(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.servePatchPricing(w, r)
}

// DELETE /api/me/charity-admin/pricing — delete a pricing entry (level 5).
func (g *Gateway) handleMePricingDelete(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveDeletePricing(w, r)
}

// POST /api/me/charity-admin/pricing/delete/batch — batch delete (level 5).
func (g *Gateway) handleMePricingBatchDelete(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveBatchDeletePricing(w, r)
}

// --- Level-5: site-wide request log (list + stats, no export) ---

// GET /api/me/all-logs — site-wide request logs (level 5). error_detail
// goes through the R-B user-view sanitizer; raw text stays admin-only.
func (g *Gateway) handleMeAllLogs(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveAllLogs(w, r, true)
}

// GET /api/me/all-logs/stats — hourly log stats (level 5).
func (g *Gateway) handleMeAllLogsStats(w http.ResponseWriter, r *http.Request) {
	if g.meLevelGuard(w, r, 5) == nil {
		return
	}
	g.serveLogStats(w, r)
}
