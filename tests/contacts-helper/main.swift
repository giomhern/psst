import Foundation
import Contacts

private final class SyntheticContactStore: ContactFetching {
    var contacts: [CNContact] = []
    var failure: Error? = nil
    var calls = 0
    var responses: [Result<[CNContact], Error>] = []
    var records: [CNContact] = []
    var scans = 0
    var scanFailure: Error? = nil
    func enumerateContacts(with request: CNContactFetchRequest, usingBlock block: (CNContact, UnsafeMutablePointer<ObjCBool>) -> Void) throws {
        scans += 1
        var stop: ObjCBool = false
        for contact in records {
            block(contact, &stop)
            if stop.boolValue { break }
        }
        if let scanFailure { throw scanFailure }
    }
    func unifiedContacts(matching predicate: NSPredicate, keysToFetch keys: [CNKeyDescriptor]) throws -> [CNContact] {
        calls += 1
        if !responses.isEmpty { return try responses.removeFirst().get() }
        if let failure { throw failure }
        return contacts
    }
}


@main
struct ContactNamesTests {
    static func main() throws {
        precondition(ContactNames.unambiguous([]) == nil)
        precondition(ContactNames.unambiguous([nil]) == nil)
        precondition(ContactNames.unambiguous(["  "]) == nil)
        precondition(ContactNames.unambiguous([" Alex "]) == "Alex")
        precondition(ContactNames.unambiguous(["Alex", "Sam"]) == nil)
        precondition(ContactNames.unambiguous(["Alex", "Alex"]) == nil)
        let long = String(repeating: "猫", count: 220)
        precondition(ContactNames.unambiguous([long])?.unicodeScalars.count == 200)
        let combining = "a" + String(repeating: "\u{301}", count: 250)
        precondition(ContactNames.unambiguous([combining])?.unicodeScalars.count == 200)
        let privateError = NSError(domain: "CNErrorDomain", code: 400,
            userInfo: [NSLocalizedDescriptionKey: "private contact details"])
        let partialStore = SyntheticContactStore()
        let alex = CNMutableContact()
        alex.givenName = "Alex"
        let sam = CNMutableContact()
        sam.givenName = "Sam"
        partialStore.responses = [.success([alex]), .failure(privateError), .success([sam])]
        let batch = ContactLookup.resolve(["b@example.test", "a@example.test", "c@example.test", "b@example.test"], store: partialStore, region: "US")
        precondition(partialStore.calls == 3)
        precondition(batch.names == ["a@example.test": "Alex", "c@example.test": "Sam"])
        precondition(batch.failure == "invalid_predicate")
        for (code, category) in [(1, "contacts_communication"), (2, "contacts_data"),
                                  (100, "contacts_access"), (102, "unauthorized_keys"), (999, "lookup_failed")] {
            precondition(ContactFailure.failureCategory(NSError(domain: "CNErrorDomain", code: code)) == category)
        }
        precondition(ContactFailure.failureCategory(NSError(domain: "private", code: 400)) == "lookup_failed")
        let store = SyntheticContactStore()
        let contact = CNMutableContact()
        contact.givenName = "Alex"
        store.contacts = [contact]
        let phoneName = try ContactLookup.name(for: "+15550000001", store: store)
        precondition(phoneName == "Alex")
        let emailName = try ContactLookup.name(for: "a@example.test", store: store)
        precondition(emailName == "Alex")
        precondition(store.calls == 2)
        let invalidName = try ContactLookup.name(for: "not-a-phone", store: store)
        precondition(invalidName == nil && store.calls == 2)
        store.contacts = []
        let missingName = try ContactLookup.name(for: "a@example.test", store: store)
        precondition(missingName == nil)
        store.contacts = [contact, contact]
        let ambiguousName = try ContactLookup.name(for: "a@example.test", store: store)
        precondition(ambiguousName == nil)
        store.failure = NSError(domain: "CNErrorDomain", code: 2)
        let failed = ContactLookup.resolve(["a@example.test"], store: store, region: "US")
        precondition(failed.names.isEmpty && failed.failure == "contacts_data")
        store.failure = nil
        store.contacts = []
        contact.phoneNumbers = [CNLabeledValue(label: nil, value: CNPhoneNumber(stringValue: "(213) 555-0100"))]
        store.records = [contact]
        let fallback = ContactLookup.resolve(["+12135550100"], store: store, region: "US")
        precondition(fallback.names["+12135550100"] == "Alex" && fallback.outcomes["matched"] == 1)
        let other = CNMutableContact()
        other.givenName = "Sam"
        other.phoneNumbers = contact.phoneNumbers
        store.records = [contact, other]
        let ambiguous = ContactLookup.resolve(["+12135550100"], store: store, region: "US")
        precondition(ambiguous.names.isEmpty && ambiguous.outcomes["ambiguous"] == 1)
        store.records = Array(repeating: contact, count: 10_001)
        let incomplete = ContactLookup.resolve(["+12135550100"], store: store, region: "US")
        precondition(incomplete.names.isEmpty && incomplete.failure != nil)
        store.records = [contact]
        store.scanFailure = NSError(domain: "CNErrorDomain", code: 2)
        let failedScan = ContactLookup.resolve(["+12135550100"], store: store, region: "US")
        precondition(failedScan.names.isEmpty && failedScan.failure == "contacts_data")
        store.scanFailure = nil
        contact.emailAddresses = [CNLabeledValue(label: nil, value: "Alex@Example.test" as NSString)]
        let emailFallback = ContactLookup.resolve(["alex@example.test"], store: store, region: nil)
        precondition(emailFallback.names["alex@example.test"] == "Alex")
        let namelessContact = CNMutableContact()
        namelessContact.phoneNumbers = contact.phoneNumbers
        store.records = [namelessContact]
        let nameless = ContactLookup.resolve(["+12135550100"], store: store, region: "US")
        precondition(nameless.names.isEmpty && nameless.outcomes["nameless"] == 1)
        store.contacts = [contact]
        let previousScans = store.scans
        let direct = ContactLookup.resolve(["+12135550100"], store: store, region: "US")
        precondition(direct.names["+12135550100"] == "Alex" && store.scans == previousScans)
        precondition(ContactLookup.key("(213) 555-0100", region: "US") == ContactLookup.key("+12135550100", region: "US"))
        precondition(ContactLookup.key("2135550100", region: "GB") != ContactLookup.key("+12135550100", region: "GB"))
        precondition(ContactLookup.key("+442135550100", region: "US") != ContactLookup.key("+12135550100", region: "US"))
        precondition(ContactLookup.key("+1+2135550100", region: "US") == nil)
        precondition(ContactLookup.key("2135550100 ext 2", region: "US") == nil)
        print("Synthetic Contacts lookup, normalization and bounded scan tests passed")
    }
}
