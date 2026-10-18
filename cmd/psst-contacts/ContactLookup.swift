import Foundation
import Contacts

// This read-only boundary lets native tests use synthetic contacts exclusively.
protocol ContactFetching {
    func unifiedContacts(matching predicate: NSPredicate, keysToFetch keys: [CNKeyDescriptor]) throws -> [CNContact]
}

extension CNContactStore: ContactFetching {}

enum ContactLookup {
    static func name(for handle: String, store: ContactFetching) throws -> String? {
        let predicate: NSPredicate
        if handle.contains("@") {
            predicate = CNContact.predicateForContacts(matchingEmailAddress: handle)
        } else {
            let allowed = CharacterSet(charactersIn: "+0123456789 ()-.")
            guard handle.unicodeScalars.allSatisfy({ allowed.contains($0) }),
                  handle.filter({ $0.isNumber }).count >= 3 else { return nil }
            predicate = CNContact.predicateForContacts(matching: CNPhoneNumber(stringValue: handle))
        }
        // Use the predicate-fetch API directly. No whole-address-book scan or
        // custom phone suffix matching is needed. Fetch only formatting keys.
        let matches = try store.unifiedContacts(matching: predicate,
            keysToFetch: [CNContactFormatter.descriptorForRequiredKeys(for: .fullName)])
        guard matches.count == 1 else { return nil }
        return ContactNames.unambiguous([CNContactFormatter.string(from: matches[0], style: .fullName)])
    }
}
