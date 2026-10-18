import Foundation

// Bound names and reject ambiguous matches before they enter the JSON protocol.
enum ContactNames {
    static func unambiguous(_ matches: [String?]) -> String? {
        guard matches.count == 1, let candidate = matches[0] else { return nil }
        let name = candidate.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { return nil }
        return String(String.UnicodeScalarView(name.unicodeScalars.prefix(200)))
    }
}
