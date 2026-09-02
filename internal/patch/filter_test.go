package patch

import (
	"bytes"
	"testing"
)

func TestFilterSelectsWholeRegionAndDemotesExcludedDeletion(t *testing.T) {
	input := []byte(`diff --git a/f b/f
--- a/f
+++ b/f
@@ -1,6 +1,6 @@
 a
-old2
+new2
 c
-old4
+new4
 e
`)
	got, err := Filter(input, []Range{{Start: 2, End: 2}})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`diff --git a/f b/f
--- a/f
+++ b/f
@@ -1,6 +1,6 @@
 a
-old2
+new2
 c
 old4
 e
`)
	if !bytes.Equal(got, want) {
		t.Fatalf("filtered patch:\n%s\nwant:\n%s", got, want)
	}
}

func TestFilterPureDeletionUsesAdjacentNewLine(t *testing.T) {
	input := []byte(`diff --git a/f b/f
--- a/f
+++ b/f
@@ -1,3 +1,2 @@
 a
-b
 c
`)
	got, err := Filter(input, []Range{{Start: 2, End: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("-b\n")) {
		t.Fatalf("pure deletion was not selected:\n%s", got)
	}
}

func TestFilterDropsNoNewlineMarkerOwnedByExcludedAddition(t *testing.T) {
	input := []byte(`diff --git a/f b/f
--- a/f
+++ b/f
@@ -1,2 +1,2 @@
-old
+new
\ No newline at end of file
 keep
`)
	got, err := Filter(input, []Range{{Start: 2, End: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("+new")) || bytes.Contains(got, []byte("No newline")) {
		t.Fatalf("excluded addition residue remains:\n%s", got)
	}
}
