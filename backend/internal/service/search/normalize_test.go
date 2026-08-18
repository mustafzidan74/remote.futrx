package search

import "testing"

func TestFold(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "latin is lower-cased", in: "Deploy The Server", want: "deploy the server"},
		{name: "whitespace collapses", in: "  a \n\t b  ", want: "a b"},
		{
			name: "tashkeel is dropped",
			in:   "مَرْحَبًا",
			want: "مرحبا",
		},
		{
			name: "tatweel is dropped",
			in:   "مــرحبا",
			want: "مرحبا",
		},
		{
			name: "hamza carriers fold onto a bare alef",
			in:   "أحمد إسلام آخر",
			want: "احمد اسلام اخر",
		},
		{
			name: "teh marbuta folds onto heh",
			in:   "مصرية",
			want: "مصريه",
		},
		{
			name: "alef maqsura folds onto yeh",
			in:   "على",
			want: "علي",
		},
		{
			name: "hamza on waw and yeh keep their base letter",
			in:   "مؤتمر مسئول",
			want: "موتمر مسيول",
		},
		{
			name: "arabic-indic digits fold onto ascii",
			in:   "٢٠٢٥",
			want: "2025",
		},
		{
			name: "zero-width joiners are dropped",
			in:   "a​b‍c",
			want: "abc",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Fold(test.in); got != test.want {
				t.Errorf("Fold(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestFoldMakesSpellingVariantsMatch(t *testing.T) {
	// The point of folding: how a user types a query and how the agent wrote
	// its answer are rarely the same string.
	pairs := [][2]string{
		{"أحمد", "احمد"},       // أحمد / احمد
		{"مصرية", "مصريه"},     // مصرية / مصريه
		{"مَرْحَبًا", "مرحبا"}, // مَرْحَبًا / مرحبا
		{"Deploy", "deploy"},
	}
	for _, pair := range pairs {
		if Fold(pair[0]) != Fold(pair[1]) {
			t.Errorf("Fold(%q)=%q != Fold(%q)=%q", pair[0], Fold(pair[0]), pair[1], Fold(pair[1]))
		}
	}
}
