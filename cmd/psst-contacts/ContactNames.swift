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

// A failed identifier must not discard successful names from the same request.
struct ContactBatch {
    var names: [String: String] = [:]
    var failure: String? = nil

    static func resolve(_ handles: [String], lookup: (String) throws -> String?) -> ContactBatch {
        var batch = ContactBatch()
        for handle in Set(handles).sorted() {
            do {
                if let name = try lookup(handle) { batch.names[handle] = name }
            } catch {
                let category = failureCategory(error)
                if batch.failure == nil { batch.failure = category }
                else if batch.failure != category { batch.failure = "lookup_failed" }
            }
        }
        return batch
    }

    // Never serialize NSError descriptions, userInfo, or record identifiers.
    static func failureCategory(_ error: Error) -> String {
        let error = error as NSError
        guard error.domain == "CNErrorDomain" else { return "lookup_failed" }
        switch error.code {
        case 1: return "contacts_communication"
        case 2: return "contacts_data"
        case 100: return "contacts_access"
        case 102: return "unauthorized_keys"
        case 400: return "invalid_predicate"
        default: return "lookup_failed"
        }
    }
}
