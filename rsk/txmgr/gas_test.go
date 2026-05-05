package txmgr

import (
	"math/big"
	"testing"
)

func TestDefaultMinLegacyGasPriceWei(t *testing.T) {
	if DefaultMinLegacyGasPriceWei.Cmp(big.NewInt(7_500_000)) != 0 {
		t.Errorf("DefaultMinLegacyGasPriceWei = %v, want 7_500_000", DefaultMinLegacyGasPriceWei)
	}
}

func TestDefaultGasPriceEstimatorNonNil(t *testing.T) {
	if DefaultGasPriceEstimator() == nil {
		t.Fatal("DefaultGasPriceEstimator() returned nil")
	}
}

func TestGasPriceEstimatorWithMinWei(t *testing.T) {
	cases := []*big.Int{nil, big.NewInt(0), big.NewInt(1_000_000_000)}
	for _, c := range cases {
		if GasPriceEstimatorWithMinWei(c) == nil {
			t.Fatalf("GasPriceEstimatorWithMinWei(%v) returned nil", c)
		}
	}
}

func TestAlreadyPublishedErrsContainsMempoolMessage(t *testing.T) {
	if len(AlreadyPublishedErrs) == 0 {
		t.Fatal("AlreadyPublishedErrs is empty")
	}
	const want = "pending transaction with same hash already exists"
	for _, m := range AlreadyPublishedErrs {
		if m == want {
			return
		}
	}
	t.Errorf("AlreadyPublishedErrs missing %q (got %v)", want, AlreadyPublishedErrs)
}
