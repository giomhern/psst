import Foundation
import Contacts

// This read-only boundary lets native tests use synthetic contacts exclusively.
protocol ContactFetching {
    func unifiedContacts(matching predicate: NSPredicate, keysToFetch keys: [CNKeyDescriptor]) throws -> [CNContact]
    func enumerateContacts(with request: CNContactFetchRequest, usingBlock block: (CNContact, UnsafeMutablePointer<ObjCBool>) -> Void) throws
}

extension CNContactStore: ContactFetching {}

enum ContactLookup {
    struct Result {
        var names: [String: String] = [:]
        var failure: String? = nil
        var outcomes: [String: Int] = [:]
    }

    enum Match {
        case named(String), missing, ambiguous, nameless, unsupported
        var outcome: String {
            switch self {
            case .named: return "matched"
            case .missing: return "missing"
            case .ambiguous: return "ambiguous"
            case .nameless: return "nameless"
            case .unsupported: return "unsupported"
            }
        }
    }

    static func name(for handle: String, store: ContactFetching) throws -> String? {
        if case .named(let name) = try match(for: handle, store: store) { return name }
        return nil
    }

    static func match(for handle: String, store: ContactFetching) throws -> Match {
        let predicate: NSPredicate
        if handle.contains("@") {
            predicate = CNContact.predicateForContacts(matchingEmailAddress: handle)
        } else {
            let allowed = CharacterSet(charactersIn: "+0123456789 ()-.")
            guard handle.unicodeScalars.allSatisfy({ allowed.contains($0) }),
                  handle.filter({ $0.isNumber }).count >= 3 else { return .unsupported }
            predicate = CNContact.predicateForContacts(matching: CNPhoneNumber(stringValue: handle))
        }
        // Try the predicate-fetch API first, fetching only formatting keys.
        // Missing matches can use the bounded normalized scan below.
        let matches = try store.unifiedContacts(matching: predicate,
            keysToFetch: [CNContactFormatter.descriptorForRequiredKeys(for: .fullName)])
        guard !matches.isEmpty else { return .missing }
        guard matches.count == 1 else { return .ambiguous }
        guard let name = ContactNames.unambiguous([CNContactFormatter.string(from: matches[0], style: .fullName)]) else { return .nameless }
        return .named(name)
    }


    static func resolve(_ handles: [String], store: ContactFetching, region: String?) -> Result {
        var result = Result()
        var matches: [String: Match] = [:]
        var missing: [String] = []
        for handle in Set(handles).sorted() {
            do {
                let match = try match(for: handle, store: store)
                matches[handle] = match
                if case .missing = match { missing.append(handle) }
            } catch {
                result.failure = ContactFailure.failureCategory(error)
                result.outcomes["failed", default: 0] += 1
            }
        }
        if !missing.isEmpty {
            do {
                let fallback = try scan(missing, store: store, region: region)
                for (handle, match) in fallback { matches[handle] = match }
            } catch {
                result.failure = ContactFailure.failureCategory(error)
            }
        }
        for (handle, match) in matches {
            result.outcomes[match.outcome, default: 0] += 1
            if case .named(let name) = match { result.names[handle] = name }
        }
        return result
    }

    // Never use suffix comparisons. Country-less NANP numbers are interpreted
    // only with a US/CA device region; other national numbers stay national.
    static func key(_ value: String, region: String?) -> String? {
        let value = value.trimmingCharacters(in: .whitespacesAndNewlines)
        if value.contains("@") { return "email:" + value.lowercased() }
        let allowed = CharacterSet(charactersIn: "+0123456789 ()-.")
        guard value.unicodeScalars.allSatisfy({ allowed.contains($0) }) else { return nil }
        let digits = value.filter { $0 >= "0" && $0 <= "9" }
        guard (7...15).contains(digits.count) else { return nil }
        if value.hasPrefix("+") && value.filter({ $0 == "+" }).count == 1 { return "phone:+" + digits }
        guard !value.contains("+") else { return nil }
        if region == "US" || region == "CA" {
            let national = digits.count == 11 && digits.first == "1" ? String(digits.dropFirst()) : digits
            if national.count == 10 {
                let chars = Array(national)
                if ("2"..."9").contains(chars[0]) && ("2"..."9").contains(chars[3]) { return "phone:+1" + national }
            }
        }
        return "phone:national:" + digits
    }

    private struct Candidate {
        let identifier: String
        let name: String?
    }

    private static func scan(_ handles: [String], store: ContactFetching, region: String?) throws -> [String: Match] {
        var requested: [String: [String]] = [:]
        for handle in handles {
            if let key = key(handle, region: region) { requested[key, default: []].append(handle) }
        }
        guard !requested.isEmpty else { return [:] }
        let request = CNContactFetchRequest(keysToFetch: [CNContactIdentifierKey as CNKeyDescriptor,
            CNContactPhoneNumbersKey as CNKeyDescriptor, CNContactEmailAddressesKey as CNKeyDescriptor,
            CNContactFormatter.descriptorForRequiredKeys(for: .fullName)])
        request.unifyResults = true
        var candidates: [String: [Candidate]] = [:]
        var count = 0
        var incomplete = false
        try store.enumerateContacts(with: request) { contact, stop in
            count += 1
            // Incomplete scans cannot establish uniqueness, so discard them.
            if count > 10_000 || contact.phoneNumbers.count > 100 || contact.emailAddresses.count > 100 {
                incomplete = true
                stop.pointee = true
                return
            }
            let values = contact.phoneNumbers.map { $0.value.stringValue } + contact.emailAddresses.map { String($0.value) }
            let keys = Set(values.compactMap { key($0, region: region) }).intersection(requested.keys)
            guard !keys.isEmpty else { return }
            let candidate = Candidate(identifier: contact.identifier,
                name: ContactNames.unambiguous([CNContactFormatter.string(from: contact, style: .fullName)]))
            for key in keys {
                var matches = candidates[key, default: []]
                if matches.count < 2 && !matches.contains(where: { $0.identifier == candidate.identifier }) {
                    matches.append(candidate)
                    candidates[key] = matches
                }
            }
        }
        guard !incomplete else { throw ScanError.limit }
        var results: [String: Match] = [:]
        for (key, handles) in requested {
            let matches = candidates[key, default: []]
            let match: Match
            if matches.isEmpty { match = .missing }
            else if matches.count > 1 { match = .ambiguous }
            else if let name = matches[0].name { match = .named(name) }
            else { match = .nameless }
            for handle in handles { results[handle] = match }
        }
        return results
    }

    private enum ScanError: Error { case limit }
}
