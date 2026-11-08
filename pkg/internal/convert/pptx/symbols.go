package pptx

// symbolFace is the face a bullet character no list level gives a face
// draws in: LibreOffice's OpenSymbol, Impress's default bullet face. A
// bullet of a symbol font (Wingdings, Symbol...) keeps its character and
// its face as the file gives them: the builder recodes it as Impress
// does.
const symbolFace = "OpenSymbol"
