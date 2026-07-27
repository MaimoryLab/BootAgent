/**
 * Agent glyphs.
 *
 * Two sources, one visual system. Claude, OpenCode and Cursor ship official
 * marks (simple-icons, CC0-1.0); Codex, Aider and Kilo have none published, so
 * they are drawn here to the same geometry. Every glyph uses a 24x24 box and
 * currentColor: a uniform container with per-brand shapes is how macOS makes a
 * folder of unrelated app icons look like one set, and rendering third-party
 * marks monochrome avoids recolouring a trademark.
 *
 * CC0 covers the SVG paths, not trademark rights. These identify which Agent a
 * row refers to — nominative use — and are not OneAgent product artwork.
 */
import { Bot } from "lucide-react";

/** Filled marks come from simple-icons; drawn ones are stroked outlines. */
type Glyph = { d: string; filled: boolean };

const GLYPHS: Record<string, Glyph> = {
  // simple-icons "claude" (CC0-1.0)
  "claude-code": {
    d: "m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z",
    filled: true,
  },
  // simple-icons "opencode" (CC0-1.0)
  opencode: {
    d: "M22 24H2V0h20zM17 4.8H7v14.4h10z",
    filled: true,
  },
  // simple-icons "cursor" (CC0-1.0)
  cursor: {
    d: "M11.503.131 1.891 5.678a.84.84 0 0 0-.42.726v11.188c0 .3.162.575.42.724l9.609 5.55a1 1 0 0 0 .998 0l9.61-5.55a.84.84 0 0 0 .42-.724V6.404a.84.84 0 0 0-.42-.726L12.497.131a1.01 1.01 0 0 0-.996 0M2.657 6.338h18.55c.263 0 .43.287.297.515L12.23 22.918c-.062.107-.229.064-.229-.06V12.335a.59.59 0 0 0-.295-.51l-9.11-5.257c-.109-.063-.064-.23.061-.23",
    filled: true,
  },
  // Drawn: a terminal prompt in a rounded frame. No official Codex mark exists.
  codex: {
    d: "M3.5 4.5h17a1.5 1.5 0 0 1 1.5 1.5v12a1.5 1.5 0 0 1-1.5 1.5h-17A1.5 1.5 0 0 1 2 18V6a1.5 1.5 0 0 1 1.5-1.5ZM7 9.5l2.5 2.5L7 14.5M12.5 15h4.5",
    filled: false,
  },
  // Drawn: paired carets, for a tool that edits alongside you.
  aider: {
    d: "M9 6 3.5 12 9 18M15 6l5.5 6L15 18M12.8 4.5l-1.6 15",
    filled: false,
  },
  // Drawn: stacked layers, matching Kilo's CLI-orchestration role.
  "kilo-cli": {
    d: "M12 2.5 21.5 7.5 12 12.5 2.5 7.5 12 2.5ZM2.5 12.2 12 17.2l9.5-5M2.5 16.6 12 21.6l9.5-5",
    filled: false,
  },
};

/** One-line positioning shown on hover; never a restatement of the name. */
const TAGLINES: Record<string, string> = {
  codex: "OpenAI 的终端编码代理",
  "claude-code": "Anthropic 的终端编码代理",
  opencode: "开源终端编码代理",
  "kilo-cli": "多模型编排的命令行代理",
  aider: "结对编程式的仓库编辑代理",
  cursor: "AI 编辑器，按官方文档配置",
};

export const AGENT_ICON_IDS = Object.keys(GLYPHS);

export function agentTagline(agentId: string): string {
  return TAGLINES[agentId] ?? "";
}

export function AgentIcon({ agentId, size = 18 }: { agentId: string; size?: number }) {
  const glyph = GLYPHS[agentId];
  if (!glyph) {
    // An Agent added to agents.lock.json before its glyph exists still renders.
    return <Bot size={size} strokeWidth={1.8} aria-hidden="true" />;
  }
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      aria-hidden="true"
      // Official marks are solid, drawn ones are strokes; the opacity keeps the
      // two from differing in visual weight inside the same list.
      fill={glyph.filled ? "currentColor" : "none"}
      fillOpacity={glyph.filled ? 0.85 : undefined}
      stroke={glyph.filled ? "none" : "currentColor"}
      strokeWidth={glyph.filled ? undefined : 1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={glyph.d} />
    </svg>
  );
}
