package filestore_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/afero"

	"github.com/konflux-ci/konflux-ci/pkg/konftool/filestore"
)

const testKey = "Some store key"

type testStruct struct {
	SomeStr    string
	SomeStrPtr *string
	SomeBool   bool
	SomeTime   time.Time
}

var _ = Describe("Filestore", func() {
	var fs afero.Fs
	var store, otherStore filestore.Filestore
	var inStruct testStruct

	BeforeEach(func() {
		fs = afero.NewMemMapFs()
		store = filestore.Filestore{Fs: fs}
		otherStore = filestore.Filestore{Fs: fs}

		otherStr := "another test string"
		inStruct = testStruct{
			SomeStr:    "Some test string",
			SomeStrPtr: &otherStr,
			SomeBool:   true,
			SomeTime:   time.Date(2024, 7, 7, 12, 45, 0, 0, time.UTC),
		}
	})
	It("is able to save and load structs", func() {
		var outStruct, otherOutStruct testStruct
		Expect(store.Put(testKey, &inStruct)).Should(Succeed())
		Expect(store.Get(testKey, &outStruct)).Should(Succeed())
		Expect(outStruct).To(Equal(inStruct))
		Expect(otherStore.Get(testKey, &otherOutStruct)).Should(Succeed())
		Expect(otherOutStruct).To(Equal(inStruct))
	})
})
