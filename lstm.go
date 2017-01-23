package charrnn

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"

	"github.com/unixpickle/anydiff/anyseq"
	"github.com/unixpickle/anynet"
	"github.com/unixpickle/anynet/anyrnn"
	"github.com/unixpickle/anynet/anys2s"
	"github.com/unixpickle/anynet/anysgd"
	"github.com/unixpickle/anyvec"
	"github.com/unixpickle/anyvec/anyvec32"
	"github.com/unixpickle/rip"
	"github.com/unixpickle/serializer"
)

func init() {
	var l LSTM
	serializer.RegisterTypedDeserializer(l.SerializerType(), DeserializeLSTM)
}

// LSTM is a Model for long short-term memory RNNs.
type LSTM struct {
	lstmTrainingFlags
	lstmGenerationFlags

	Block anyrnn.Block
}

func DeserializeLSTM(d []byte) (*LSTM, error) {
	var b anyrnn.Block
	if err := serializer.DeserializeAny(d, &b); err != nil {
		return nil, err
	}
	return &LSTM{Block: b}, nil
}

func (l *LSTM) Train(samples SampleList) {
	if l.Block == nil {
		l.createModel()
	}

	t := &anys2s.Trainer{
		Func: func(s anyseq.Seq) anyseq.Seq {
			return anyrnn.Map(s, l.Block)
		},
		Cost:    anynet.DotCost{},
		Params:  l.Block.(anynet.Parameterizer).Parameters(),
		Average: true,
	}

	var iter int
	sgd := &anysgd.SGD{
		Fetcher:     t,
		Gradienter:  t,
		Transformer: &anysgd.Adam{},
		Samples: &anys2s.SortSampleList{
			SortableSampleList: samples,
			BatchSize:          l.SortBatch,
		},
		Rater: anysgd.ConstRater(l.StepSize),
		StatusFunc: func(b anysgd.Batch) {
			log.Printf("iter %d: cost=%v", iter, t.LastCost)
			iter++
		},
		BatchSize: l.BatchSize,
	}

	log.Println("Training (ctrl+c to stop)...")
	sgd.Run(rip.NewRIP().Chan())
}

func (l *LSTM) Generate() {
	state := l.Block.Start(1)

	last := anyvec32.MakeVector(CharCount)
	for i := 0; i < l.Length; i++ {
		res := l.Block.Step(state, last)
		ch := sampleSoftmax(res.Output())

		fmt.Print(string([]byte{byte(ch)}))

		v := make([]float32, CharCount)
		v[ch] = 1
		last = anyvec32.MakeVectorData(v)
		state = res.State()
	}

	fmt.Println()
}

func (l *LSTM) Name() string {
	return "lstm"
}

func (l *LSTM) SerializerType() string {
	return "github.com/unixpickle/char-rnn.LSTM"
}

func (l *LSTM) Serialize() ([]byte, error) {
	return serializer.SerializeAny(l.Block)
}

func (l *LSTM) createModel() {
	block := anyrnn.Stack{}
	inCount := CharCount
	for i := 0; i < l.Layers; i++ {
		block = append(block, anyrnn.NewLSTM(anyvec32.CurrentCreator(),
			inCount, l.Hidden))
		inCount = l.Hidden
	}
	block = append(block, &anyrnn.LayerBlock{
		Layer: anynet.Net{
			anynet.NewFC(anyvec32.CurrentCreator(), inCount, CharCount),
			anynet.LogSoftmax,
		},
	})
	var size int
	for _, p := range block.Parameters() {
		size += p.Vector.Len()
	}
	l.Block = block
}

type lstmTrainingFlags struct {
	StepSize  float64
	Hidden    int
	Layers    int
	BatchSize int
	SortBatch int
}

func (l *lstmTrainingFlags) TrainingFlags() *flag.FlagSet {
	res := flag.NewFlagSet("lstm", flag.ExitOnError)
	res.IntVar(&l.Hidden, "hidden", 512, "hidden neuron count")
	res.IntVar(&l.Layers, "layers", 2, "LSTM layer count")
	res.Float64Var(&l.StepSize, "step", 0.001, "step size")
	res.IntVar(&l.BatchSize, "batch", 32, "SGD batch size")
	res.IntVar(&l.SortBatch, "sortbatch", 128, "sample sort batch size")
	return res
}

type lstmGenerationFlags struct {
	Length int
}

func (l *lstmGenerationFlags) GenerationFlags() *flag.FlagSet {
	res := flag.NewFlagSet("lstm", flag.ExitOnError)
	res.IntVar(&l.Length, "length", 100, "generated string length")
	return res
}

func sampleSoftmax(vec anyvec.Vector) int {
	p := rand.Float64()
	for i, x := range vec.Data().([]float32) {
		p -= math.Exp(float64(x))
		if p < 0 {
			return i
		}
	}
	return CharCount - 1
}
