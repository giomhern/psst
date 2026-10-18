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
        print("Synthetic Contacts helper name tests passed")
    }
}
