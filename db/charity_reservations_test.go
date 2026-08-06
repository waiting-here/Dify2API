package db

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func reservationFixture(t *testing.T, totalCount, consumerCredits int) (*Store, *User, *User, *Donation) {
	t.Helper()
	store, _ := openTemp(t)
	consumer, err := store.CreateUser("reservation-consumer", "consumer", "")
	if err != nil {
		t.Fatal(err)
	}
	donor, err := store.CreateUser("reservation-donor", "donor", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserCredits(consumer.ID, consumerCredits); err != nil {
		t.Fatal(err)
	}
	donation, err := store.CreateDonation(&Donation{
		Service: "general", Model: "reservation-model", DifyBaseURL: "https://dify.example.com",
		SourceUserID: sql.NullInt64{Int64: donor.ID, Valid: true},
		Deadline:     time.Now().Add(time.Hour).Unix(), TotalCount: totalCount, Status: DonationActive,
	}, "app-key")
	if err != nil {
		t.Fatal(err)
	}
	return store, consumer, donor, donation
}

func TestCharityReservation_CommitIsAtomicAndIdempotent(t *testing.T) {
	store, consumer, donor, donation := reservationFixture(t, 2, 100)
	ctx := context.Background()
	reservation, err := store.ReserveCharityCall(ctx, consumer.ID, donation.ID, 30, 11)
	if err != nil {
		t.Fatal(err)
	}
	gotConsumer, _ := store.GetUserByID(consumer.ID)
	gotDonation, _ := store.GetDonation(donation.ID)
	if gotConsumer.Credits != 70 || gotDonation.RemainingCount != 1 || gotDonation.SuccessCount != 0 {
		t.Fatalf("after reserve: credits=%d remaining=%d success=%d", gotConsumer.Credits, gotDonation.RemainingCount, gotDonation.SuccessCount)
	}
	if err := store.MarkCharityDispatched(ctx, reservation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitCharityReservation(ctx, reservation.ID); err != nil {
		t.Fatal(err)
	}
	// A retry after an uncertain commit must not double-credit the donor.
	if _, err := store.CommitCharityReservation(ctx, reservation.ID); err != nil {
		t.Fatal(err)
	}
	gotDonation, _ = store.GetDonation(donation.ID)
	gotConsumer, _ = store.GetUserByID(consumer.ID)
	gotDonor, _ := store.GetUserByID(donor.ID)
	if gotDonation.RemainingCount != 1 || gotDonation.SuccessCount != 1 {
		t.Fatalf("settled donation = remaining %d success %d", gotDonation.RemainingCount, gotDonation.SuccessCount)
	}
	if gotConsumer.Credits != 70 || gotDonor.Credits != 11 || gotDonor.DonationCredit != 1 {
		t.Fatalf("settled balances consumer=%d donor=%d contribution=%d", gotConsumer.Credits, gotDonor.Credits, gotDonor.DonationCredit)
	}
	consumerExport, err := store.ExportUserData(consumer.ID)
	if err != nil || len(consumerExport.CharityReservations) != 1 || consumerExport.CharityReservations[0].Role != "consumer" {
		t.Fatalf("consumer reservation export = %+v, err=%v", consumerExport.CharityReservations, err)
	}
	donorExport, err := store.ExportUserData(donor.ID)
	if err != nil || len(donorExport.CharityReservations) != 1 || donorExport.CharityReservations[0].Role != "donor" {
		t.Fatalf("donor reservation export = %+v, err=%v", donorExport.CharityReservations, err)
	}
}

func TestCharityReservation_ReleaseReservedRefundsAndRestores(t *testing.T) {
	store, consumer, _, donation := reservationFixture(t, 1, 20)
	ctx := context.Background()
	reservation, err := store.ReserveCharityCall(ctx, consumer.ID, donation.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetDonation(donation.ID)
	if got.RemainingCount != 0 || got.Status != DonationExpired {
		t.Fatalf("last use should be reserved/expired: %+v", got)
	}
	if err := store.DeleteDonation(donation.ID); err == nil {
		t.Fatal("in-flight donation must not be deleted")
	}
	consecutive, err := store.ReleaseCharityReservation(ctx, reservation.ID, false)
	if err != nil || consecutive != 0 {
		t.Fatalf("release = consecutive %d, err %v", consecutive, err)
	}
	// Idempotent retry cannot refund twice.
	if _, err := store.ReleaseCharityReservation(ctx, reservation.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetDonation(donation.ID)
	gotConsumer, _ := store.GetUserByID(consumer.ID)
	if got.RemainingCount != 1 || got.Status != DonationActive || got.FailureCount != 0 || got.ConsecutiveFailures != 0 || gotConsumer.Credits != 20 {
		t.Fatalf("released state: donation=%+v credits=%d", got, gotConsumer.Credits)
	}
}

func TestCharityReservation_DispatchedCannotBeReleased(t *testing.T) {
	store, consumer, donor, donation := reservationFixture(t, 2, 20)
	ctx := context.Background()
	reservation, err := store.ReserveCharityCall(ctx, consumer.ID, donation.ID, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCharityDispatched(ctx, reservation.ID); err != nil {
		t.Fatal(err)
	}

	// Even a legacy caller asking to count a donation failure cannot cross the
	// dispatch boundary by refunding the reservation.
	if _, err := store.ReleaseCharityReservation(ctx, reservation.ID, true); err == nil {
		t.Fatal("dispatched reservation was released")
	}
	gotReservation, _ := store.GetCharityReservation(reservation.ID)
	gotDonation, _ := store.GetDonation(donation.ID)
	gotConsumer, _ := store.GetUserByID(consumer.ID)
	if gotReservation.Status != ReservationDispatched || gotDonation.RemainingCount != 1 ||
		gotDonation.SuccessCount != 0 || gotDonation.FailureCount != 0 || gotDonation.ConsecutiveFailures != 0 || gotConsumer.Credits != 10 {
		t.Fatalf("release rejection mutated state: reservation=%+v donation=%+v consumer=%+v", gotReservation, gotDonation, gotConsumer)
	}

	if _, err := store.CommitCharityReservation(ctx, reservation.ID); err != nil {
		t.Fatal(err)
	}
	// Repeating either settlement cannot double-apply it or reverse it.
	if _, err := store.CommitCharityReservation(ctx, reservation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReleaseCharityReservation(ctx, reservation.ID, true); err == nil {
		t.Fatal("committed reservation was released")
	}
	gotReservation, _ = store.GetCharityReservation(reservation.ID)
	gotDonation, _ = store.GetDonation(donation.ID)
	gotConsumer, _ = store.GetUserByID(consumer.ID)
	gotDonor, _ := store.GetUserByID(donor.ID)
	if gotReservation.Status != ReservationCommitted || gotDonation.RemainingCount != 1 ||
		gotDonation.SuccessCount != 1 || gotDonation.FailureCount != 0 || gotDonation.ConsecutiveFailures != 0 ||
		gotConsumer.Credits != 10 || gotDonor.Credits != 3 || gotDonor.DonationCredit != 1 {
		t.Fatalf("commit settlement mismatch: reservation=%+v donation=%+v consumer=%+v donor=%+v", gotReservation, gotDonation, gotConsumer, gotDonor)
	}
}

func TestCharityReservation_ConcurrentCreditAndDonationCaps(t *testing.T) {
	t.Run("credits", func(t *testing.T) {
		store, consumer, _, donation := reservationFixture(t, 50, 10)
		reservations, insufficient, other := reserveConcurrently(store, consumer.ID, donation.ID, 10, 0, 50)
		if len(reservations) != 1 || insufficient != 49 || other != 0 {
			t.Fatalf("success=%d insufficient=%d other=%d", len(reservations), insufficient, other)
		}
		user, _ := store.GetUserByID(consumer.ID)
		gotDonation, _ := store.GetDonation(donation.ID)
		if user.Credits != 0 || gotDonation.RemainingCount != 49 {
			t.Fatalf("credits=%d remaining=%d", user.Credits, gotDonation.RemainingCount)
		}
	})

	t.Run("remaining", func(t *testing.T) {
		store, consumer, _, donation := reservationFixture(t, 1, 100)
		reservations, unavailable, other := reserveConcurrently(store, consumer.ID, donation.ID, 1, 0, 50)
		if len(reservations) != 1 || unavailable != 49 || other != 0 {
			t.Fatalf("success=%d unavailable=%d other=%d", len(reservations), unavailable, other)
		}
		user, _ := store.GetUserByID(consumer.ID)
		gotDonation, _ := store.GetDonation(donation.ID)
		if user.Credits != 99 || gotDonation.RemainingCount != 0 {
			t.Fatalf("credits=%d remaining=%d", user.Credits, gotDonation.RemainingCount)
		}
	})
}

func reserveConcurrently(store *Store, userID, donationID int64, price, reward, count int) (reservations []*CharityReservation, expectedErrs, otherErrs int) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reservation, err := store.ReserveCharityCall(context.Background(), userID, donationID, price, reward)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				reservations = append(reservations, reservation)
			case errors.Is(err, ErrInsufficientCredits), errors.Is(err, ErrDonationUnavailable):
				expectedErrs++
			default:
				otherErrs++
			}
		}()
	}
	wg.Wait()
	return reservations, expectedErrs, otherErrs
}

func TestCharityReservation_Recovery(t *testing.T) {
	store, consumer, donor, donation := reservationFixture(t, 3, 30)
	ctx := context.Background()
	reserved, err := store.ReserveCharityCall(ctx, consumer.ID, donation.ID, 10, 4)
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := store.ReserveCharityCall(ctx, consumer.ID, donation.ID, 10, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCharityDispatched(ctx, dispatched.ID); err != nil {
		t.Fatal(err)
	}
	released, committed, err := store.RecoverCharityReservations(ctx)
	if err != nil || released != 1 || committed != 1 {
		t.Fatalf("recovery released=%d committed=%d err=%v", released, committed, err)
	}
	// Re-running recovery after an interrupted startup is a no-op.
	released, committed, err = store.RecoverCharityReservations(ctx)
	if err != nil || released != 0 || committed != 0 {
		t.Fatalf("second recovery released=%d committed=%d err=%v", released, committed, err)
	}
	reserved, _ = store.GetCharityReservation(reserved.ID)
	dispatched, _ = store.GetCharityReservation(dispatched.ID)
	if reserved.Status != ReservationReleased || dispatched.Status != ReservationCommitted {
		t.Fatalf("statuses: reserved=%s dispatched=%s", reserved.Status, dispatched.Status)
	}
	gotConsumer, _ := store.GetUserByID(consumer.ID)
	gotDonor, _ := store.GetUserByID(donor.ID)
	gotDonation, _ := store.GetDonation(donation.ID)
	if gotConsumer.Credits != 20 || gotDonor.Credits != 4 || gotDonor.DonationCredit != 1 ||
		gotDonation.RemainingCount != 2 || gotDonation.SuccessCount != 1 || gotDonation.FailureCount != 0 || gotDonation.ConsecutiveFailures != 0 {
		t.Fatalf("recovered balances: consumer=%d donor=%d contribution=%d donation=%+v", gotConsumer.Credits, gotDonor.Credits, gotDonor.DonationCredit, gotDonation)
	}
}
