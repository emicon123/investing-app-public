package assetlifecycle

import (
	"errors"
	"testing"
)

func TestPlanSubassetDelete(t *testing.T) {
	cases := []struct {
		name      string
		isPrimary bool
		live      int
		want      DeleteAction
	}{
		{"primary with live siblings", true, 2, DeleteConflict},
		{"primary with many live siblings", true, 5, DeleteConflict},
		{"last live subasset, primary", true, 1, DeleteSoftDeleteAsset},
		{"last live subasset, non-primary (defensive)", false, 1, DeleteSoftDeleteAsset},
		{"non-primary with siblings", false, 3, DeleteSoftDeleteSubasset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlanSubassetDelete(tc.isPrimary, tc.live); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPlanCreate(t *testing.T) {
	liveA := &AssetState{ID: 10, HasLivePrimary: true}
	liveNoPrimary := &AssetState{ID: 11}
	deletedA := &AssetState{ID: 12, Deleted: true}
	deletedK := &AssetState{ID: 13, Deleted: true}
	liveK := &AssetState{ID: 14, HasLivePrimary: true}
	livePos := &RowState{ID: 100, AssetID: 10, IsPrimary: false}
	deletedPosInDeletedAsset := &RowState{ID: 101, AssetID: 12, Deleted: true, IsPrimary: true}
	deletedPosInLiveAsset := &RowState{ID: 102, AssetID: 10, Deleted: true}

	cases := []struct {
		name    string
		in      CreateInput
		want    CreatePlan
		wantErr error
	}{
		{name: "live duplicate (assetId mode)",
			in: CreateInput{TargetAsset: liveA, Position: livePos}, wantErr: ErrDuplicate},
		{name: "live duplicate (newAsset mode)",
			in: CreateInput{NewAsset: true, Position: livePos, PositionAsset: liveA}, wantErr: ErrDuplicate},
		{name: "assetId missing",
			in: CreateInput{TargetAsset: nil}, wantErr: ErrAssetNotFound},
		{name: "assetId deleted",
			in: CreateInput{TargetAsset: deletedA}, wantErr: ErrAssetNotFound},
		{name: "primary taken",
			in: CreateInput{TargetAsset: liveA, WantPrimary: true}, wantErr: ErrPrimaryTaken},
		{name: "assetId insert non-primary",
			in:   CreateInput{TargetAsset: liveA},
			want: CreatePlan{Asset: AssetUseExisting, AssetID: 10, Subasset: SubassetInsert}},
		{name: "assetId insert primary into primary-less asset",
			in:   CreateInput{TargetAsset: liveNoPrimary, WantPrimary: true},
			want: CreatePlan{Asset: AssetUseExisting, AssetID: 11, Subasset: SubassetInsert, IsPrimary: true}},
		{name: "deleted P + assetId restores into target",
			in:   CreateInput{TargetAsset: liveA, Position: deletedPosInDeletedAsset, PositionAsset: deletedA},
			want: CreatePlan{Asset: AssetUseExisting, AssetID: 10, Subasset: SubassetRestore, SubassetID: 101}},
		{name: "deleted P + assetId still checks the target",
			in: CreateInput{TargetAsset: nil, Position: deletedPosInDeletedAsset, PositionAsset: deletedA}, wantErr: ErrAssetNotFound},
		{name: "deleted P + newAsset with deleted asset restores P's asset",
			in: CreateInput{NewAsset: true, Position: deletedPosInDeletedAsset, PositionAsset: deletedA, KeyAsset: liveK},
			want: CreatePlan{Asset: AssetRestore, AssetID: 12, Subasset: SubassetRestore, SubassetID: 101,
				IsPrimary: true, ClearPrimaries: true}},
		{name: "deleted P in live asset + newAsset, key hits deleted asset",
			in: CreateInput{NewAsset: true, Position: deletedPosInLiveAsset, PositionAsset: liveA, KeyAsset: deletedK},
			want: CreatePlan{Asset: AssetRestore, AssetID: 13, Subasset: SubassetRestore, SubassetID: 102,
				IsPrimary: true, ClearPrimaries: true}},
		{name: "deleted P in live asset + newAsset, key free",
			in:   CreateInput{NewAsset: true, Position: deletedPosInLiveAsset, PositionAsset: liveA},
			want: CreatePlan{Asset: AssetInsert, Subasset: SubassetRestore, SubassetID: 102, IsPrimary: true}},
		{name: "deleted P in live asset + newAsset, key live",
			in: CreateInput{NewAsset: true, Position: deletedPosInLiveAsset, PositionAsset: liveA, KeyAsset: liveK}, wantErr: ErrDuplicate},
		{name: "no P, key hits deleted asset",
			in: CreateInput{NewAsset: true, KeyAsset: deletedK},
			want: CreatePlan{Asset: AssetRestore, AssetID: 13, Subasset: SubassetInsert,
				IsPrimary: true, ClearPrimaries: true}},
		{name: "no P, key live",
			in: CreateInput{NewAsset: true, KeyAsset: liveK}, wantErr: ErrDuplicate},
		{name: "nothing found",
			in:   CreateInput{NewAsset: true},
			want: CreatePlan{Asset: AssetInsert, Subasset: SubassetInsert, IsPrimary: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PlanCreate(tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
