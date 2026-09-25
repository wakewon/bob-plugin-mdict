package service_test

import (
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
)

// The bilingual Collins layout writes the numbers of a translated sentence as
// bare text between the gloss spans, not inside them. They must reach the IR
// translation, or the sentence is left with a hole where the number was.
const syntheticGlossNumbersHTML = `<div class="collinsbody"><div class="word_entry"><span class="word_key">flimber</span></div>
<div class="collins_content"><div class="collins_en_cn example">
<div class="caption"><a class="anchor" name="flimber_1"></a><span class="num">1</span> <span class="st">N-COUNT </span><span class="def_cn cn_before"><span class="chinese-text">合成物</span></span> An invented thing used only in tests.</div>
<ul>
<li><p>A flimber lasts seven <span class="text_blue">days</span>.<a class="tts_button"> </a></p><p><span class="chinese-text">一个合成物能用</span> 7<span class="chinese-text">天。</span></p></li>
<li><p>It began in 1989 and ended in 2001.</p><p>1989<span class="chinese-text">年开始，</span>2001<span class="chinese-text">年结束。</span></p></li>
<li><p>There were 12 people and it cost 50 coins.</p><p><span class="chinese-text">共有</span>12<span class="chinese-text">人，花费</span>50<span class="chinese-text">枚硬币。</span></p></li>
<li><p>Prices rose by 30%.</p><p><span class="chinese-text">价格上涨了</span>30%</p></li>
</ul></div></div></div>`

func TestCollinsGlossNumbersSurviveInIR(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{{Key: "flimber", HTML: syntheticGlossNumbersHTML}})
	match := lookupSynthetic(t, svc, "flimber", service.LookupOptions{Mode: service.ModeExact})

	entry := match.Records[0].Entry
	if entry == nil || entry.Source.Profile != "collins-cobuild-overhaul" {
		t.Fatalf("fixture no longer selects the Collins profile: %+v", entry)
	}
	if len(entry.Parts) != 1 || len(entry.Parts[0].Senses) != 1 {
		t.Fatalf("unexpected structure: %+v", entry.Parts)
	}
	sense := entry.Parts[0].Senses[0]
	if sense.Number != "1" || sense.Translation != "合成物" {
		t.Fatalf("sense number/translation = %q/%q, want 1/合成物", sense.Number, sense.Translation)
	}

	want := []struct{ text, translation string }{
		{"A flimber lasts seven days.", "一个合成物能用 7天。"},
		{"It began in 1989 and ended in 2001.", "1989年开始，2001年结束。"},
		{"There were 12 people and it cost 50 coins.", "共有12人，花费50枚硬币。"},
		{"Prices rose by 30%.", "价格上涨了30%"},
	}
	if len(sense.Examples) != len(want) {
		t.Fatalf("examples = %+v", sense.Examples)
	}
	for i, example := range sense.Examples {
		if example.Text != want[i].text || example.Translation != want[i].translation {
			t.Errorf("example %d = %q / %q, want %q / %q", i, example.Text, example.Translation, want[i].text, want[i].translation)
		}
	}
}
