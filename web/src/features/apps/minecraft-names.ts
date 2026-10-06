// names reads a list of Minecraft names, split by commas, spaces or lines.
export function names(text: string) {
    return text.split(/[\s,]+/).filter(Boolean)
}
