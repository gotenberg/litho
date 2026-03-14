// Package models contains the decoupled, pure-Go data structures representing PDF elements.
//
// The structs defined in this package (such as Pages, Annotations, FormFields, and Bookmarks)
// are completely isolated from the internal Abstract Syntax Tree (AST). They provide a safe,
// stable, and easy-to-use public interface for developers integrating the litho engine,
// ensuring that downstream consumers do not need to understand or link against internal PDF object dictionaries or indirect references.
package models
