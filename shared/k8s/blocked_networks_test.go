package k8s

import (
	"reflect"
	"testing"
)

// TestParseBlockedNetworks は、カンマ区切りの CIDR を、検査して、ネットワークの形にそろえることを確認する。
func TestParseBlockedNetworks(testingT *testing.T) {
	testCases := []struct {
		name          string
		text          string
		expected      []string
		expectedError bool
	}{
		{name: "空なら既定", text: "", expected: DefaultBlockedNetworks()},
		{name: "空白だけでも既定", text: "  ", expected: DefaultBlockedNetworks()},
		{name: "1つ", text: "192.168.0.0/16", expected: []string{"192.168.0.0/16"}},
		{name: "複数(空白あり。順は指定のまま)", text: "203.0.113.0/24 , 10.0.0.0/8", expected: []string{"203.0.113.0/24", "10.0.0.0/8"}},
		{name: "末尾のカンマと空の要素は無視する", text: "10.0.0.0/8,,192.168.0.0/16,", expected: []string{"10.0.0.0/8", "192.168.0.0/16"}},
		{name: "重複は1つにする", text: "10.0.0.0/8,10.0.0.0/8", expected: []string{"10.0.0.0/8"}},
		{name: "ネットワークの形に直す(ホスト部があっても)", text: "10.1.2.3/8", expected: []string{"10.0.0.0/8"}},
		{name: "1つのホストも指定できる(/32)", text: "203.0.113.5/32", expected: []string{"203.0.113.5/32"}},
		{name: "CIDR として読めなければエラー", text: "not-a-cidr", expectedError: true},
		{name: "マスクが無ければエラー", text: "10.0.0.0", expectedError: true},
		{name: "IPv6 はエラー", text: "2001:db8::/32", expectedError: true},
		{name: "1つでも不正なら全体がエラー(正しいものがあっても)", text: "10.0.0.0/8,bad", expectedError: true},
		{name: "0.0.0.0/0(全て)はエラー", text: "0.0.0.0/0", expectedError: true},
		{name: "0.0.0.0/0 は、他の要素と一緒でもエラー", text: "10.0.0.0/8,0.0.0.0/0", expectedError: true},
		{name: "カンマだけで、要素が残らなければエラー", text: ",,", expectedError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			networks, parseError := ParseBlockedNetworks(testCase.text)
			if (parseError != nil) != testCase.expectedError {
				subTest.Fatalf("エラーの有無が違う: %v", parseError)
			}
			if !testCase.expectedError && !reflect.DeepEqual(networks, testCase.expected) {
				subTest.Errorf("期待 %v、実際 %v", testCase.expected, networks)
			}
		})
	}
}

// TestDefaultBlockedNetworks は、既定が決めた5つで、呼び出し側が書き換えても、既定の値が変わらないことを確認する。
func TestDefaultBlockedNetworks(testingT *testing.T) {
	first := DefaultBlockedNetworks()
	if !reflect.DeepEqual(first, []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10"}) {
		testingT.Errorf("決めた5つのはず: %v", first)
	}
	first[0] = "changed"
	if second := DefaultBlockedNetworks(); second[0] != "10.0.0.0/8" {
		testingT.Errorf("書き換えの影響が出てはいけない: %v", second)
	}
}
