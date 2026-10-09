import type { MultimodalCapabilityId } from "../types/api";

/**
 * Prompts the user copies into an Agent to try a newly installed Skill. Both
 * languages are shown side by side whatever the UI locale, because the Agent
 * may be used in either; they are content, not UI copy, so they do not go
 * through translate().
 */
export interface ExamplePrompt {
  zh: string;
  en: string;
}

export const EXAMPLE_PROMPTS: Partial<Record<MultimodalCapabilityId, ExamplePrompt[]>> = {
  vision: [
    { zh: "看一下 ./screenshot.png，找出界面上的报错信息并告诉我原因。", en: "Look at ./screenshot.png and find the error message in the UI, then tell me what caused it." },
    { zh: "对比 ./before.png 和 ./after.png，列出界面上有哪些变化。", en: "Compare ./before.png and ./after.png and list what changed in the UI." },
    { zh: "读一下 ./diagram.png 里的架构图，用文字描述各个组件之间的关系。", en: "Read the architecture diagram in ./diagram.png and describe how the components relate." },
  ],
  "image-generation": [
    { zh: "生成一张极简风格的应用图标草图：一只纸飞机，蓝白配色。生成前先告诉我会花费多少张。", en: "Generate a minimalist app icon draft: a paper plane in blue and white. Tell me how many images it will make before you run it." },
    { zh: "为 README 生成一张横幅插图，主题是“本地优先的开发工具”，先和我确认提示词再生成。", en: "Generate a banner illustration for the README on the theme \"local-first developer tools\". Confirm the prompt with me first." },
  ],
  "speech-to-text": [
    { zh: "把 ./meeting.m4a 转成文字，然后整理出待办事项列表。", en: "Transcribe ./meeting.m4a, then turn it into a list of action items." },
    { zh: "转写 ./voice-note.mp3，录音是中文，保留原话不要改写。", en: "Transcribe ./voice-note.mp3. It is in Chinese; keep the wording as spoken." },
  ],
  "video-generation": [
    { zh: "生成一段 5 秒的视频：一架纸飞机穿过清晨的城市。生成前先告诉我时长和分辨率，等我确认。", en: "Make a 5-second video of a paper plane flying through a city at dawn. Tell me the duration and resolution and wait for my go-ahead first." },
    { zh: "为产品发布做一段 5 秒的竖屏短片，主题是“本地优先”。先把提示词和参数给我看。", en: "Make a 5-second vertical clip for a product launch on the theme \"local-first\". Show me the prompt and settings first." },
  ],
  "text-to-speech": [
    { zh: "把这次改动的总结读成一段 mp3 语音，保存到 ./summary.mp3。先给我看要朗读的文字。", en: "Turn a summary of this change into an mp3 and save it as ./summary.mp3. Show me the text first." },
    { zh: "用语音合成把 README 的第一段生成为音频，生成前先确认。", en: "Make an audio file of the first paragraph of the README with text-to-speech. Confirm before you run it." },
  ],
};
