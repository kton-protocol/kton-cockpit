package watch

import "testing"

// mh3, 12.11.: ark fetched jsonlite's sources from CRAN into ~/.cache/oak while data-prep.R ran in the
// console; the connection is the session's background, not the step's
func TestArkBackground_CRANFetchIntoOakIsNotTheStepsConnection(t *testing.T) {
	s := NewSession("positron-1", "/log.tsv", &Classifier{System: DefaultSystem, Temp: DefaultTemp, Project: "/p"})
	s.Line("#ktontrace v3\thash=/p")
	s.Line("10\t200\t1\tTASK\tok\t1\t0\t0\t0\tsource(\"script/data-prep.R\")")
	s.Line("1000000000\t200\t1\tconnect\tnet\tinet:137.208.57.37:443\t0\t0\t0\tclass=remote;rc=-1;errno=115")
	s.Line("1070000000\t200\t1\topen\tw\t/home/u/.cache/oak/source/v1/cran/jsonlite_2.0.0/tests/x.R\t0\t1\t2\tnew=1")
	s.Line("1100000000\t200\t1\tconnect\tnet\tinet:10.0.0.9:443\t0\t0\t0\tclass=remote;rc=0;errno=0")
	s.Line("9000000000\t200\t1\tEND\tok\t1\t0\t0\t0\t-")
	var nets []Net
	for _, tk := range s.Tasks {
		nets = append(nets, tk.Network...)
	}
	if len(nets) != 2 || nets[0].Class != "background" || nets[1].Class != "remote" {
		t.Fatalf("nets %+v: the CRAN fetch before ark's cache write is background, a later connection stays the step's", nets)
	}
}
