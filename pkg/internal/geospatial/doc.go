// Package geospatial extracts geospatial metadata from PDFs that use the
// /Measure dictionary with /Subtype /GEO. It collects coordinate systems,
// tie points (PDF-to-GPS mappings), and bounds, and returns them as
// decoupled [models.GeoData] values. A helper on [models.GeoData] serializes
// the data to GeoJSON.
package geospatial
