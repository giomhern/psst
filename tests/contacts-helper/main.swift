import Foundation

@main
struct ContactNamesTests {
    static func main() {
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
        var requests: [String] = []
        let batch = ContactBatch.resolve(["b@example.test", "a@example.test", "c@example.test", "b@example.test"]) { handle in
            requests.append(handle)
            if handle == "b@example.test" { throw privateError }
            return handle == "a@example.test" ? "Alex" : "Sam"
        }
        precondition(requests == ["a@example.test", "b@example.test", "c@example.test"])
        precondition(batch.names == ["a@example.test": "Alex", "c@example.test": "Sam"])
        precondition(batch.failure == "invalid_predicate")
        for (code, category) in [(1, "contacts_communication"), (2, "contacts_data"),
                                  (100, "contacts_access"), (102, "unauthorized_keys"), (999, "lookup_failed")] {
            precondition(ContactBatch.failureCategory(NSError(domain: "CNErrorDomain", code: code)) == category)
        }
        precondition(ContactBatch.failureCategory(NSError(domain: "private", code: 400)) == "lookup_failed")
        print("Synthetic Contacts helper name and partial-failure tests passed")
    }
}
