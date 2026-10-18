import Foundation
import Contacts

private struct Request: Decodable {
    let v: Int
    let handles: [String]
}

private struct Response: Encodable {
    let v = 1
    let status: String
    var names: [String: String] = [:]
    var failure: String? = nil
}

@main
private struct ContactsHelper {
    static func status() -> String {
        switch CNContactStore.authorizationStatus(for: .contacts) {
        case .notDetermined: return "permission_required"
        case .restricted: return "restricted"
        case .denied: return "denied"
        case .authorized: return "authorized"
        case .limited: return "limited"
        @unknown default: return "unavailable"
        }
    }

    static func emit(_ response: Response) {
        // Never print NSError descriptions or records outside this bounded protocol.
        guard let data = try? JSONEncoder().encode(response) else { return }
        FileHandle.standardOutput.write(data)
    }

    static func request() throws -> Request {
        var input = Data()
        while let chunk = try FileHandle.standardInput.read(upToCount: 65536), !chunk.isEmpty {
            input.append(chunk)
            if input.count > 1_048_576 { throw CocoaError(.fileReadTooLarge) }
        }
        let request = try JSONDecoder().decode(Request.self, from: input)
        guard request.v == 1, request.handles.count <= 500,
              request.handles.allSatisfy({ !$0.isEmpty && $0.utf8.count <= 320 }) else {
            throw CocoaError(.coderReadCorrupt)
        }
        return request
    }

    static func name(for handle: String, store: CNContactStore) throws -> String? {
        let predicate: NSPredicate
        if handle.contains("@") {
            predicate = CNContact.predicateForContacts(matchingEmailAddress: handle)
        } else {
            let allowed = CharacterSet(charactersIn: "+0123456789 ()-.")
            guard handle.unicodeScalars.allSatisfy({ allowed.contains($0) }),
                  handle.filter({ $0.isNumber }).count >= 3 else { return nil }
            predicate = CNContact.predicateForContacts(matching: CNPhoneNumber(stringValue: handle))
        }
        let request = CNContactFetchRequest(keysToFetch: [CNContactFormatter.descriptorForRequiredKeys(for: .fullName)])
        request.predicate = predicate
        request.unifyResults = true
        var matches: [String?] = []
        try store.enumerateContacts(with: request) { contact, stop in
            matches.append(CNContactFormatter.string(from: contact, style: .fullName))
            if matches.count >= 2 { stop.pointee = true }
        }
        return ContactNames.unambiguous(matches)
    }

    static func main() async {
        let args = Array(CommandLine.arguments.dropFirst())
        if args == ["--help"] {
            print("Psst Contacts helper: status | authorize | lookup (bounded JSON on stdin)")
            return
        }
        guard args.count == 1 else { emit(Response(status: "unavailable")); return }
        do {
            switch args[0] {
            case "status": emit(Response(status: status()))
            case "authorize":
                if status() == "permission_required" {
                    _ = try await CNContactStore().requestAccess(for: .contacts)
                }
                emit(Response(status: status()))
            case "lookup":
                let request = try request() // Validate before touching authorization or Contacts.
                let authorization = status()
                guard authorization == "authorized" || authorization == "limited" else {
                    emit(Response(status: authorization)); return
                }
                let store = CNContactStore()
                let batch = ContactBatch.resolve(request.handles) { handle in
                    try name(for: handle, store: store)
                }
                emit(Response(status: authorization, names: batch.names, failure: batch.failure))
            default: emit(Response(status: "unavailable"))
            }
        } catch { emit(Response(status: "unavailable", failure: "request_invalid")) }
    }
}
