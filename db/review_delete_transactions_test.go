package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func reviewTransactionFixture(t *testing.T) (*Store, *User, *User, *DonationApplication) {
	t.Helper()
	st, _ := openTemp(t)
	applicant, err := st.CreateUser("review-applicant", "applicant", "")
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := st.CreateUser("review-reviewer", "reviewer", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err := st.CreateDonationApplication(
		applicant.ID, "general", "review-model", "https://dify.example.com/v1",
		"app-review-key", 10, time.Now().Add(time.Hour).Unix(), 10, "review fixture",
	)
	if err != nil {
		t.Fatal(err)
	}
	return st, applicant, reviewer, app
}

func TestApplicationReview_ConcurrentApproveCreatesOneDonation(t *testing.T) {
	st, _, reviewer, app := reviewTransactionFixture(t)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := st.ApproveApplication(app.ID, reviewer.ID, &ApproveApplicationFields{}, "ok")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		var stateErr *ApplicationReviewError
		if errors.As(err, &stateErr) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected approval error: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
	var donations int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM donations`).Scan(&donations); err != nil {
		t.Fatal(err)
	}
	if donations != 1 {
		t.Fatalf("donations=%d, want 1", donations)
	}
}

func TestApplicationReview_ConcurrentApproveRejectHasOneTerminalState(t *testing.T) {
	st, _, reviewer, app := reviewTransactionFixture(t)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, err := st.ApproveApplication(app.ID, reviewer.ID, &ApproveApplicationFields{}, "approve")
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := st.RejectApplication(app.ID, reviewer.ID, "reject")
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		var stateErr *ApplicationReviewError
		if !errors.As(err, &stateErr) {
			t.Fatalf("unexpected review error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d, want 1", successes)
	}
	got, err := st.GetApplication(app.ID)
	if err != nil || got == nil || (got.Status != AppStatusApproved && got.Status != AppStatusRejected) {
		t.Fatalf("terminal application=%+v, err=%v", got, err)
	}
	var donations int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM donations`).Scan(&donations); err != nil {
		t.Fatal(err)
	}
	wantDonations := 0
	if got.Status == AppStatusApproved {
		wantDonations = 1
	}
	if donations != wantDonations {
		t.Fatalf("status=%s donations=%d, want %d", got.Status, donations, wantDonations)
	}
}

func TestApproveApplications_MidTransactionFailureRollsBack(t *testing.T) {
	st, applicant, reviewer, first := reviewTransactionFixture(t)
	second, err := st.CreateDonationApplication(
		applicant.ID, "general", "review-model-2", "https://dify.example.com/v1",
		"app-review-key-2", 10, time.Now().Add(time.Hour).Unix(), 10, "second",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(fmt.Sprintf(`CREATE TRIGGER fail_second_review
		BEFORE UPDATE OF status ON donation_applications
		WHEN OLD.id=%d AND NEW.status='approved'
		BEGIN SELECT RAISE(ABORT, 'forced review failure'); END`, second.ID)); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveApplications([]int64{first.ID, second.ID}, reviewer.ID, "batch"); err == nil {
		t.Fatal("batch approval succeeded despite injected failure")
	}
	for _, id := range []int64{first.ID, second.ID} {
		got, getErr := st.GetApplication(id)
		if getErr != nil || got == nil || got.Status != AppStatusPending || got.DonationID.Valid {
			t.Fatalf("application %d after rollback = %+v, err=%v", id, got, getErr)
		}
	}
	var donations int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM donations`).Scan(&donations); err != nil {
		t.Fatal(err)
	}
	if donations != 0 {
		t.Fatalf("donations=%d after rollback, want 0", donations)
	}
}

func TestCreateDonationApplicationWithLimit_ConcurrentCap(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("pending-limit-user", "pending", "")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := st.CreateDonationApplicationWithLimit(
				u.ID, "general", fmt.Sprintf("pending-%d", i), "https://dify.example.com/v1",
				fmt.Sprintf("pending-key-%d", i), 10, time.Now().Add(time.Hour).Unix(), 10, "", 1,
			)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	limited := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrPendingApplicationLimit):
			limited++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if successes != 1 || limited != 3 {
		t.Fatalf("successes=%d limited=%d, want 1/3", successes, limited)
	}
	if pending, err := st.CountPendingByUser(u.ID); err != nil || pending != 1 {
		t.Fatalf("pending=%d, err=%v, want 1", pending, err)
	}
}

func TestCreateDonationApplicationWithLimit_ZeroDisablesSubmissions(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("pending-zero-user", "pending", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateDonationApplicationWithLimit(
		u.ID, "general", "pending-zero", "https://dify.example.com/v1", "pending-zero-key",
		10, time.Now().Add(time.Hour).Unix(), 10, "", 0,
	)
	if !errors.Is(err, ErrPendingApplicationLimit) {
		t.Fatalf("zero limit error=%v, want ErrPendingApplicationLimit", err)
	}
}

func TestDeleteUser_MidTransactionFailureRollsBack(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("delete-rollback", "rollback", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateSession(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAppConfig(u.ID, "[general]delete-rollback", "https://dify.example.com/v1", "config-key", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCallerKey(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDonationApplication(
		u.ID, "general", "delete-rollback-app", "https://dify.example.com/v1",
		"delete-rollback-app-key", 10, time.Now().Add(time.Hour).Unix(), 10, "",
	); err != nil {
		t.Fatal(err)
	}
	logID, err := st.AddRequestLogFull(
		u.ID, "delete-rollback-log", "general", time.Now(), time.Now(), "error", "x", 500, "detail", 0, 0, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddAdminAlert(&AdminAlert{Type: "delete-rollback-test", Message: "bound", RequestLogID: &logID}); err != nil {
		t.Fatal(err)
	}
	donation, err := st.CreateDonation(&Donation{
		Service: "general", Model: "delete-rollback-model", DifyBaseURL: "https://dify.example.com/v1",
		SourceUserID: sql.NullInt64{Int64: u.ID, Valid: true}, Deadline: time.Now().Add(time.Hour).Unix(), TotalCount: 10,
	}, "delete-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER fail_user_delete
		BEFORE DELETE ON users WHEN OLD.id=` + fmt.Sprint(u.ID) + `
		BEGIN SELECT RAISE(ABORT, 'forced user delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(u.ID); err == nil {
		t.Fatal("DeleteUser succeeded despite injected failure")
	}
	if got, err := st.GetUserByID(u.ID); err != nil || got == nil {
		t.Fatalf("user was not restored: got=%+v err=%v", got, err)
	}
	var sessions int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id=?`, u.ID).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("sessions=%d err=%v, want 1", sessions, err)
	}
	for table, query := range map[string]string{
		"app_configs":           `SELECT COUNT(*) FROM app_configs WHERE user_id=?`,
		"caller_keys":           `SELECT COUNT(*) FROM caller_keys WHERE user_id=?`,
		"request_logs":          `SELECT COUNT(*) FROM request_logs WHERE user_id=?`,
		"donation_applications": `SELECT COUNT(*) FROM donation_applications WHERE user_id=?`,
	} {
		var count int
		if err := st.db.QueryRow(query, u.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d, err=%v, want 1", table, count, err)
		}
	}
	var alerts int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM admin_alerts WHERE request_log_id=?`, logID).Scan(&alerts); err != nil || alerts != 1 {
		t.Fatalf("alerts=%d err=%v, want 1", alerts, err)
	}
	gotDonation, err := st.GetDonation(donation.ID)
	if err != nil || gotDonation == nil || !gotDonation.SourceUserID.Valid || gotDonation.SourceUserID.Int64 != u.ID {
		t.Fatalf("donation source after rollback = %+v, err=%v", gotDonation, err)
	}
}

func TestDeleteUser_CleansBoundAlertsAndReviewerReference(t *testing.T) {
	st, _ := openTemp(t)
	target, err := st.CreateUser("delete-reviewer", "reviewer", "")
	if err != nil {
		t.Fatal(err)
	}
	applicant, err := st.CreateUser("delete-review-applicant", "applicant", "")
	if err != nil {
		t.Fatal(err)
	}
	app, err := st.CreateDonationApplication(
		applicant.ID, "general", "reviewer-reference", "https://dify.example.com/v1",
		"reviewer-key", 10, time.Now().Add(time.Hour).Unix(), 10, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RejectApplication(app.ID, target.ID, "reviewed"); err != nil {
		t.Fatal(err)
	}
	logID, err := st.AddRequestLogFull(
		target.ID, "delete-log", "general", time.Now(), time.Now(), "error", "x", 500, "detail", 0, 0, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddAdminAlert(&AdminAlert{Type: "delete-user-test", Message: "bound", RequestLogID: &logID}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(target.ID); err != nil {
		t.Fatal(err)
	}
	gotApp, err := st.GetApplication(app.ID)
	if err != nil || gotApp == nil || gotApp.Status != AppStatusRejected || gotApp.ReviewerID.Valid {
		t.Fatalf("preserved application=%+v, err=%v", gotApp, err)
	}
	for table, query := range map[string]string{
		"request_logs": `SELECT COUNT(*) FROM request_logs WHERE id=?`,
		"admin_alerts": `SELECT COUNT(*) FROM admin_alerts WHERE request_log_id=?`,
	} {
		var count int
		if err := st.db.QueryRow(query, logID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d, err=%v, want 0", table, count, err)
		}
	}
}

func TestDeleteUser_PreservesSettledReservationByAnonymizing(t *testing.T) {
	st, consumer, donor, donation := reservationFixture(t, 1, 20)
	reservation, err := st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCharityDispatched(context.Background(), reservation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitCharityReservation(context.Background(), reservation.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(consumer.ID); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetCharityReservation(reservation.ID)
	if err != nil || got == nil || got.UserID != 0 || !got.DonorUserID.Valid || got.DonorUserID.Int64 != donor.ID || got.Status != ReservationCommitted {
		t.Fatalf("anonymized reservation=%+v, err=%v", got, err)
	}
}

func TestDeleteUser_AdministratorIsUntouched(t *testing.T) {
	st, _ := openTemp(t)
	admin, err := st.EnsureAdminUser("admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateSession(admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(admin.ID); err == nil {
		t.Fatal("administrator deletion succeeded")
	}
	if got, err := st.GetUserByID(admin.ID); err != nil || got == nil || !got.IsAdmin {
		t.Fatalf("administrator changed: got=%+v err=%v", got, err)
	}
	var sessions int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id=?`, admin.ID).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("admin sessions=%d err=%v, want 1", sessions, err)
	}
}

func TestDeleteUserAndReserve_AreSerialized(t *testing.T) {
	st, consumer, _, donation := reservationFixture(t, 1, 20)
	start := make(chan struct{})
	var deleteErr, reserveErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		deleteErr = st.DeleteUser(consumer.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, reserveErr = st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 1, 0)
	}()
	close(start)
	wg.Wait()
	if (deleteErr == nil) == (reserveErr == nil) {
		t.Fatalf("deleteErr=%v reserveErr=%v, want exactly one success", deleteErr, reserveErr)
	}
}

func TestDeleteDonationAndReserve_AreSerialized(t *testing.T) {
	st, consumer, _, donation := reservationFixture(t, 1, 20)
	start := make(chan struct{})
	var deleteErr, reserveErr error
	var reservation *CharityReservation
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		deleteErr = st.DeleteDonation(donation.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		reservation, reserveErr = st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 1, 0)
	}()
	close(start)
	wg.Wait()
	if (deleteErr == nil) == (reserveErr == nil) {
		t.Fatalf("deleteErr=%v reserveErr=%v, want exactly one success", deleteErr, reserveErr)
	}
	if reservation != nil {
		if got, err := st.GetDonation(donation.ID); err != nil || got == nil {
			t.Fatalf("reserved donation was deleted: got=%+v err=%v", got, err)
		}
	}
}
